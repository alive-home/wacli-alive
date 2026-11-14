package app

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types/events"
)

func completeRecoveryOnRequest(f *fakeWA) {
	f.onAppStateRecovery = func(name string) {
		go f.emit(&events.AppStateSyncComplete{Name: appstate.WAPatchName(name), Version: 81, Recovery: true})
	}
}

func recoveredCollections(f *fakeWA) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.appStateRecoveries...)
}

func TestKnownEmptyKeySyncErrorRecoversOnlyAffectedCollection(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	completeRecoveryOnRequest(f)
	f.appStateFetchErr = wa.ErrEmptyAppStateKeyShare
	var recoveries sync.Map
	event := &events.AppStateSyncError{Name: appstate.WAPatchRegularHigh, Error: fmt.Errorf("decode: %w", wa.ErrEmptyAppStateKeyShare)}
	a.handleAppStateSyncError(t.Context(), event, &recoveries)
	a.appStateRecoveryWorkers.Wait()
	a.handleAppStateSyncError(t.Context(), event, &recoveries)
	a.handleAppStateSyncError(t.Context(), &events.AppStateSyncError{Name: appstate.WAPatchRegularLow, Error: appstate.ErrKeyNotFound}, &recoveries)
	a.appStateRecoveryWorkers.Wait()
	if got := recoveredCollections(f); !slices.Equal(got, []string{"regular_high"}) {
		t.Fatalf("recoveries=%v", got)
	}
}
