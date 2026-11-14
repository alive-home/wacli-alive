package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	// How long a background continuation keeps waiting for a recovery the
	// primary answers after the synchronous waiter gave up. Real phones have
	// been observed to answer 71 seconds and even hours later.
	lateAppStateRecoveryMaxWait = 24 * time.Hour
	// Minimum spacing between background recovery requests sent by the
	// degraded (full-sync-known-broken) pre-write path, per collection.
	backgroundAppStateRecoveryInterval = 6 * time.Hour
)

// AddChatStatePersistenceHandler stores the app-state events the client
// dispatches, the way a running sync does, without syncing. No command uses
// it: it is how tests, wa_test's against the real SDK client among them,
// drive app-state persistence on its own.
func (a *App) AddChatStatePersistenceHandler(ctx context.Context) (func(), error) {
	if err := a.OpenWA(); err != nil {
		return nil, err
	}
	waClient := a.WA()
	handlerID := waClient.AddEventHandler(func(evt any) {
		switch v := evt.(type) {
		case *events.AppState, *events.Star, *events.DeleteForMe,
			*events.Archive, *events.UnarchiveChatsSetting, *events.Pin, *events.Mute, *events.MarkChatAsRead,
			*events.Contact:
			a.handleAppStatePersistenceEvent(ctx, evt, nil)
		case *wa.AppStateKeyUnavailable:
			a.warnEmptyAppStateKey(v)
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			waClient.RemoveEventHandler(handlerID)
		})
	}, nil
}

func (a *App) acquireChatStateSync(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for chat state synchronization: %w", ctx.Err())
	case <-a.chatStateSync:
	}
	release := func() { a.chatStateSync <- struct{}{} }
	if err := ctx.Err(); err != nil {
		release()
		return nil, fmt.Errorf("wait for chat state synchronization: %w", err)
	}
	return release, nil
}

// setAppStateFullSyncResult records whether the latest full sync of the
// collection verified. On accounts hitting persistent LTHash mismatches a full
// sync can never verify its snapshot, so the flag stays set until a full sync
// verifies again. The flag is mirrored durably in wacli.db: stock whatsmeow's
// full sync deletes the saved collection version before decoding, so the next
// process must know the collection is broken before it starts another
// destructive full sync.
func (a *App) setAppStateFullSyncResult(collection appstate.WAPatchName, broken bool) {
	a.appStateDegradedMu.Lock()
	a.loadAppStateFullSyncResultsLocked()
	if a.appStateFullSyncBroken == nil {
		a.appStateFullSyncBroken = make(map[string]bool)
	}
	changed := a.appStateFullSyncBroken[string(collection)] != broken
	a.appStateFullSyncBroken[string(collection)] = broken
	a.appStateDegradedMu.Unlock()
	if !changed {
		return
	}
	// Best-effort persistence: this process is still protected by the
	// in-memory flag, and the next failed full sync re-arms it.
	if err := a.db.SetAppStateFullSyncBroken(string(collection), broken); err != nil {
		a.warnAppStateRecovery(string(collection), fmt.Errorf("persist full sync verification flag: %w", err))
	}
}

func (a *App) isAppStateFullSyncBroken(collection appstate.WAPatchName) bool {
	a.appStateDegradedMu.Lock()
	defer a.appStateDegradedMu.Unlock()
	a.loadAppStateFullSyncResultsLocked()
	return a.appStateFullSyncBroken[string(collection)]
}

// loadAppStateFullSyncResultsLocked hydrates the in-memory flags from
// wacli.db once per process. On a load failure the process simply behaves
// like today's main (full replay first) until a failed full sync re-arms the
// flag, so the error is not fatal.
func (a *App) loadAppStateFullSyncResultsLocked() {
	if a.appStateFullSyncLoaded {
		return
	}
	a.appStateFullSyncLoaded = true
	collections, err := a.db.AppStateFullSyncBrokenCollections()
	if err != nil {
		a.emitWarning("app_state_full_sync_flags_load_failed",
			fmt.Sprintf("warning: failed to load app state full sync verification flags: %v", err),
			map[string]any{"error": err.Error()})
		return
	}
	if a.appStateFullSyncBroken == nil {
		a.appStateFullSyncBroken = make(map[string]bool, len(collections))
	}
	for _, collection := range collections {
		a.appStateFullSyncBroken[collection] = true
	}
}

// requestBackgroundAppStateRecovery sends one recovery request to the primary
// without waiting for the answer, rate-limited per collection. whatsmeow
// adopts a late answer on its own when the snapshot is newer than the local
// state. The capture persists it in order and clears only covered replay debt.
func (a *App) requestBackgroundAppStateRecovery(ctx context.Context, collection appstate.WAPatchName, markerGeneration int64) {
	name := string(collection)
	now := time.Now()
	a.appStateDegradedMu.Lock()
	if last, ok := a.appStateRecoveryRequested[name]; ok && now.Sub(last) < backgroundAppStateRecoveryInterval {
		a.appStateDegradedMu.Unlock()
		return
	}
	if a.appStateRecoveryRequested == nil {
		a.appStateRecoveryRequested = make(map[string]time.Time)
	}
	a.appStateRecoveryRequested[name] = now
	a.appStateDegradedMu.Unlock()

	capture := a.newAppStateRecoveryCapture(collection)
	requestID, err := a.wa.RequestAppStateRecovery(ctx, name)
	if err != nil {
		capture.stop()
		a.warnAppStateRecovery(name, fmt.Errorf("background recovery request: %w", err))
		return
	}
	a.adoptLateAppStateRecovery(collection, markerGeneration, capture)
	a.emitWarning("app_state_recovery_requested_background",
		fmt.Sprintf("app state %s: requested recovery snapshot from the primary in the background (id %s)", name, requestID),
		map[string]any{"name": name, "id": string(requestID), "generation": markerGeneration})
}

func (a *App) recoverMismatchingAppState(ctx context.Context, collection appstate.WAPatchName, markerGeneration int64, tracker *appStatePersistenceTracker, onRequested func(types.MessageID)) error {
	ticket := a.appStatePersist.reserve()
	eventsToPersist, lateCapture, recoveryErr := a.waitForPrimaryAppStateRecovery(ctx, collection, onRequested)
	if lateCapture != nil {
		// The request reached the primary but the answer did not arrive in
		// time. Keep the capture alive in a background continuation so a late
		// answer still gets persisted and clears this attempt's markers.
		a.adoptLateAppStateRecovery(collection, markerGeneration, lateCapture)
	}
	persistCtx := context.WithoutCancel(ctx)
	result := make(chan error, 1)
	frontier := a.appStatePersist.complete(ticket, func() {
		if recoveryErr != nil {
			result <- nil
			return
		}
		result <- a.persistFetchedAppStateEvents(persistCtx, eventsToPersist, tracker)
	})
	if err := a.appStatePersist.waitThrough(persistCtx, frontier); err != nil {
		return err
	}
	if persistenceErr := <-result; persistenceErr != nil {
		return fmt.Errorf("persist recovered app state %s: %w", collection, persistenceErr)
	}
	if recoveryErr != nil {
		return recoveryErr
	}
	return a.clearCompletedAppStateRecovery(collection, markerGeneration)
}

// appStateRecoveryCapture buffers this collection's app state events until the
// recovery's AppStateSyncComplete sentinel arrives. Its event handler only
// appends to a slice and signals a buffered channel, so whatsmeow's dispatch
// never blocks on persistence.
type appStateRecoveryCapture struct {
	app        *App
	collection appstate.WAPatchName
	handlerID  uint32
	mu         sync.Mutex
	captured   []any
	finished   bool
	completed  chan appStateRecoveryCompletion
}

// appStateRecoveryCompletion carries the captured snapshot events together
// with the persistence ticket reserved at the dispatch boundary. Reserving
// inside the sentinel's event handler fixes the snapshot's position in the
// shared persistence order before whatsmeow can hand a newer live event to
// the persistence handler, so a late snapshot can never be replayed after a
// newer live mutation that overtook a sleeping continuation goroutine.
type appStateRecoveryCompletion struct {
	events []any
	ticket uint64
}

func (a *App) newAppStateRecoveryCapture(collection appstate.WAPatchName) *appStateRecoveryCapture {
	c := &appStateRecoveryCapture{app: a, collection: collection, completed: make(chan appStateRecoveryCompletion, 1)}
	// whatsmeow dispatches recovery mutations synchronously and emits this
	// collection's AppStateSyncComplete last, so the sentinel closes the drain.
	c.handlerID = a.wa.AddEventHandler(c.handle)
	return c
}

func (c *appStateRecoveryCapture) handle(evt any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return
	}
	if syncComplete, ok := evt.(*events.AppStateSyncComplete); ok {
		if syncComplete != nil && syncComplete.Recovery && syncComplete.Name == c.collection {
			c.finished = true
			// Reserve the snapshot's ordered write position here, at the
			// dispatch boundary. reserve only increments a counter under a
			// mutex, so whatsmeow's dispatch never blocks on persistence.
			c.completed <- appStateRecoveryCompletion{
				events: append([]any(nil), c.captured...),
				ticket: c.app.appStatePersist.reserve(),
			}
		}
		return
	}
	if slices.Contains(appStateCollectionsForEvent(evt), c.collection) {
		c.captured = append(c.captured, evt)
	}
}

// stop finishes the capture and removes its event handler. Safe to call more
// than once. A completion nobody consumed still owns a persistence ticket
// reserved at the dispatch boundary; release it so later writes keep draining.
func (c *appStateRecoveryCapture) stop() {
	c.mu.Lock()
	c.finished = true
	c.mu.Unlock()
	c.app.wa.RemoveEventHandler(c.handlerID)
	select {
	case completion := <-c.completed:
		c.app.appStatePersist.complete(completion.ticket, func() {})
	default:
	}
}

// waitForPrimaryAppStateRecovery requests a recovery snapshot from the primary
// and waits for the captured events. When the wait times out after the request
// was sent, the still-registered capture is returned so the caller can hand it
// to a background continuation instead of dropping a late answer.
func (a *App) waitForPrimaryAppStateRecovery(ctx context.Context, collection appstate.WAPatchName, onRequested func(types.MessageID)) ([]any, *appStateRecoveryCapture, error) {
	capture := a.newAppStateRecoveryCapture(collection)
	requestID, err := a.wa.RequestAppStateRecovery(ctx, string(collection))
	if err != nil {
		capture.stop()
		return nil, nil, fmt.Errorf("request WhatsApp app state recovery for %s: %w", collection, err)
	}
	if onRequested != nil {
		onRequested(requestID)
	}
	select {
	case completion := <-capture.completed:
		capture.stop()
		// The timely waiter persists under the ticket its caller reserved
		// before the request was sent; release the dispatch-boundary ticket
		// without work so the queue keeps draining.
		a.appStatePersist.complete(completion.ticket, func() {})
		return completion.events, nil, nil
	case <-ctx.Done():
		return nil, capture, fmt.Errorf("wait for WhatsApp app state recovery for %s: %w", collection, ctx.Err())
	}
}

// lateAppStateRecovery is a background continuation of a recovery attempt
// whose synchronous waiter timed out after the request was sent.
type lateAppStateRecovery struct {
	capture    *appStateRecoveryCapture
	generation int64
	superseded chan struct{}
}

// adoptLateAppStateRecovery registers at most one continuation per collection;
// a newer attempt's higher generation supersedes an older one. The capture is
// dropped when the app is closing or an equal-or-newer continuation exists.
func (a *App) adoptLateAppStateRecovery(collection appstate.WAPatchName, generation int64, capture *appStateRecoveryCapture) {
	a.appStateRecoveryMu.Lock()
	if a.appStateRecoveryClosing {
		a.appStateRecoveryMu.Unlock()
		capture.stop()
		return
	}
	if existing := a.lateRecoveries[string(collection)]; existing != nil {
		if existing.generation >= generation {
			a.appStateRecoveryMu.Unlock()
			capture.stop()
			return
		}
		close(existing.superseded)
		delete(a.lateRecoveries, string(collection))
	}
	if a.lateRecoveries == nil {
		a.lateRecoveries = make(map[string]*lateAppStateRecovery)
	}
	if a.lateRecoveryStop == nil {
		a.lateRecoveryStop = make(chan struct{})
	}
	cont := &lateAppStateRecovery{capture: capture, generation: generation, superseded: make(chan struct{})}
	a.lateRecoveries[string(collection)] = cont
	stop := a.lateRecoveryStop
	a.appStateRecoveryWorkers.Go(func() {
		a.runLateAppStateRecovery(collection, cont, stop)
	})
	a.appStateRecoveryMu.Unlock()
}

func (a *App) runLateAppStateRecovery(collection appstate.WAPatchName, cont *lateAppStateRecovery, stop <-chan struct{}) {
	timer := time.NewTimer(lateAppStateRecoveryMaxWait)
	defer timer.Stop()
	var completion appStateRecoveryCompletion
	select {
	case completion = <-cont.capture.completed:
	case <-cont.superseded:
		// The superseding attempt already removed this entry from the registry.
		cont.capture.stop()
		return
	case <-stop:
		a.finishLateAppStateRecovery(collection, cont)
		return
	case <-timer.C:
		a.finishLateAppStateRecovery(collection, cont)
		return
	}
	a.finishLateAppStateRecovery(collection, cont)
	// Same ordered persistence as the timely path. The write position was
	// reserved when the sentinel was dispatched, so a newer live event that
	// arrived after the snapshot persists after it, even though this goroutine
	// runs outside whatsmeow's dispatch and holds no chatStateSync.
	persistCtx := context.Background()
	tracker := &appStatePersistenceTracker{}
	result := make(chan error, 1)
	frontier := a.appStatePersist.complete(completion.ticket, func() {
		result <- a.persistFetchedAppStateEvents(persistCtx, completion.events, tracker)
	})
	if err := a.appStatePersist.waitThrough(persistCtx, frontier); err != nil {
		a.warnAppStateRecovery(string(collection), err)
		return
	}
	if persistenceErr := <-result; persistenceErr != nil {
		a.warnAppStateRecovery(string(collection), fmt.Errorf("persist late recovered app state %s: %w", collection, persistenceErr))
		return
	}
	if err := a.clearCompletedAppStateRecovery(collection, cont.generation); err != nil {
		a.warnAppStateRecovery(string(collection), err)
		return
	}
	a.emitWarning("app_state_recovery_completed_late",
		fmt.Sprintf("App state %s recovered late from primary", collection),
		map[string]any{"name": string(collection), "generation": cont.generation})
}

func (a *App) finishLateAppStateRecovery(collection appstate.WAPatchName, cont *lateAppStateRecovery) {
	cont.capture.stop()
	a.appStateRecoveryMu.Lock()
	if a.lateRecoveries[string(collection)] == cont {
		delete(a.lateRecoveries, string(collection))
	}
	a.appStateRecoveryMu.Unlock()
}

func (a *App) fetchAndPersistAppState(ctx context.Context, collection appstate.WAPatchName, fullSync bool, tracker *appStatePersistenceTracker) (fetchErr, persistenceErr error) {
	ticket := a.appStatePersist.reserve()
	var eventsToPersist []any
	func() {
		releaseFetch := a.beginManualAppStateFetch(collection)
		defer releaseFetch()
		eventsToPersist, fetchErr = a.wa.FetchAppStateEvents(ctx, string(collection), fullSync, false)
	}()
	// Track whether full syncs of this collection verify. Set on an LTHash
	// mismatch, cleared when a full sync verifies again.
	if fullSync {
		if fetchErr == nil {
			a.setAppStateFullSyncResult(collection, false)
		} else if errors.Is(fetchErr, appstate.ErrMismatchingLTHash) {
			a.setAppStateFullSyncResult(collection, true)
		}
	}
	result := make(chan error, 1)
	frontier := a.appStatePersist.complete(ticket, func() {
		persistCtx := ctx
		if collection == appstate.WAPatchCriticalUnblockLow {
			persistCtx = context.WithoutCancel(ctx)
		}
		persistErr := a.persistFetchedAppStateEvents(persistCtx, eventsToPersist, tracker)
		if collection == appstate.WAPatchCriticalUnblockLow {
			// Drain even without events or after fetch cancellation: SDK store
			// writes precede event delivery and must retain their canonical JID.
			drainErr := a.drainContactJournal(persistCtx)
			persistErr = errors.Join(persistErr, drainErr)
		}
		result <- persistErr
	})
	if waitErr := a.appStatePersist.waitThrough(ctx, frontier); waitErr != nil {
		return fetchErr, waitErr
	}
	return fetchErr, <-result
}

func (a *App) beginManualAppStateFetch(collection appstate.WAPatchName) func() {
	name := string(collection)
	a.manualFetchMu.Lock()
	if a.manualFetches == nil {
		a.manualFetches = make(map[string]int)
	}
	a.manualFetches[name]++
	a.manualFetchMu.Unlock()
	return func() {
		a.manualFetchMu.Lock()
		a.manualFetches[name]--
		if a.manualFetches[name] == 0 {
			delete(a.manualFetches, name)
		}
		a.manualFetchMu.Unlock()
	}
}

func (a *App) ownsManualAppStateFetch(collection appstate.WAPatchName) bool {
	a.manualFetchMu.Lock()
	defer a.manualFetchMu.Unlock()
	return a.manualFetches[string(collection)] > 0
}

func (a *App) clearCompletedAppStateRecovery(collection appstate.WAPatchName, markerGeneration int64) error {
	cleared, err := a.db.ClearAppStateRecoveryGeneration(string(collection), markerGeneration)
	if err != nil {
		return fmt.Errorf("clear WhatsApp app state recovery for %s: %w", collection, err)
	}
	if !cleared {
		required, checkErr := a.db.AppStateRecoveryRequired(string(collection))
		if checkErr != nil {
			return fmt.Errorf("check completed WhatsApp app state recovery for %s: %w", collection, checkErr)
		}
		if required {
			return fmt.Errorf("WhatsApp app state recovery changed during %s synchronization; retry the command", collection)
		}
	}
	return nil
}

func (a *App) persistFetchedAppStateEvents(ctx context.Context, eventsToPersist []any, tracker *appStatePersistenceTracker) error {
	tracker.begin()
	for _, evt := range eventsToPersist {
		a.persistAppStateEvent(ctx, evt, tracker)
	}
	return tracker.end()
}

func (a *App) handleChatStateEvent(ctx context.Context, evt any) error {
	switch v := evt.(type) {
	case *events.Archive:
		if v == nil || v.JID.IsEmpty() || v.Action == nil {
			return nil
		}
		chat := a.canonicalStoreJID(ctx, v.JID)
		if err := a.db.ApplyChatArchive(canonicalJIDString(chat), v.Action.GetArchived(), v.Timestamp); err != nil {
			a.emitChatStateWarning("archive", v.JID, err)
			return err
		}
	case *events.UnarchiveChatsSetting:
		if v == nil || v.Action == nil || v.Action.UnarchiveChats == nil {
			return nil
		}
		return a.db.SetChatArchiveSetting(v.Action.GetUnarchiveChats(), v.Timestamp)
	case *events.Pin:
		if v == nil || v.JID.IsEmpty() || v.Action == nil {
			return nil
		}
		chat := a.canonicalStoreJID(ctx, v.JID)
		if err := a.db.SetChatPinned(canonicalJIDString(chat), v.Action.GetPinned()); err != nil {
			a.emitChatStateWarning("pin", v.JID, err)
			return err
		}
	case *events.Mute:
		if v == nil || v.JID.IsEmpty() || v.Action == nil {
			return nil
		}
		chat := a.canonicalStoreJID(ctx, v.JID)
		if err := a.db.SetChatMutedUntil(canonicalJIDString(chat), mutedUntilFromAction(v.Action)); err != nil {
			a.emitChatStateWarning("mute", v.JID, err)
			return err
		}
	case *events.MarkChatAsRead:
		if v == nil || v.JID.IsEmpty() || v.Action == nil {
			return nil
		}
		chat := a.canonicalStoreJID(ctx, v.JID)
		var err error
		if v.Action.GetRead() {
			through, ids := markChatAsReadPosition(v)
			err = a.clearChatUnreadFromEvent(canonicalJIDString(chat), through, ids)
		} else {
			err = a.db.SetChatUnread(canonicalJIDString(chat), true)
		}
		if err != nil {
			a.emitChatStateWarning("mark_read", v.JID, err)
			return err
		}
	}
	return nil
}

// Contact events only trigger a journal drain: asynchronous event payloads
// cannot establish the order of SDK contact-store writes.
func (a *App) handleContactEvent(ctx context.Context, _ *events.Contact) error {
	return a.drainContactJournal(ctx)
}

func mutedUntilFromAction(action *waSyncAction.MuteAction) int64 {
	if action == nil || !action.GetMuted() {
		return 0
	}
	ms := action.GetMuteEndTimestamp()
	if ms < 0 {
		return -1
	}
	if ms > 0 {
		return time.UnixMilli(ms).Unix()
	}
	return -1
}

func (a *App) emitChatStateWarning(kind string, jid types.JID, err error) {
	a.emitWarning(
		"chat_state_store_failed",
		fmt.Sprintf("warning: failed to store %s chat state for %s: %v", kind, jid, err),
		map[string]any{"kind": kind, "jid": jid.String(), "error": err.Error()},
	)
}
