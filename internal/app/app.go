package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openclaw/wacli/internal/fsutil"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/store"
	"github.com/openclaw/wacli/internal/wa"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type WAClient interface {
	Close()
	Disconnect()
	IsAuthed() bool
	IsConnected() bool
	SetAutoReconnect(enabled bool) (previous bool, ok bool)
	Connect(ctx context.Context, opts wa.ConnectOptions) error

	AddEventHandler(handler func(any)) uint32
	RemoveEventHandler(id uint32)
	ReconnectWithBackoff(ctx context.Context, minDelay, maxDelay time.Duration, opts wa.ConnectOptions) error

	ResolveChatName(ctx context.Context, chat types.JID, pushName string) string
	ResolveLIDToPN(ctx context.Context, jid types.JID) types.JID
	ResolvePNToLID(ctx context.Context, jid types.JID) types.JID
	GetUserInfo(ctx context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error)
	IsOnWhatsApp(ctx context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error)
	GetContact(ctx context.Context, jid types.JID) (types.ContactInfo, error)
	GetAllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error)

	GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error)
	GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error)
	GetGroupRequestParticipants(ctx context.Context, group types.JID) ([]types.GroupParticipantRequest, error)
	GetGroupInviteLink(ctx context.Context, group types.JID) (string, error)

	GetNewsletterInfoWithInvite(ctx context.Context, key string) (*types.NewsletterMetadata, error)
	GetSubscribedNewsletters(ctx context.Context) ([]*types.NewsletterMetadata, error)
	GetNewsletterInfo(ctx context.Context, jid types.JID) (*types.NewsletterMetadata, error)

	DecryptPollVote(ctx context.Context, evt *events.Message) (*waE2E.PollVoteMessage, error)
	DecryptSecretEncryptedMessage(ctx context.Context, evt *events.Message) (*waE2E.Message, error)
	DownloadMediaToFile(ctx context.Context, directPath string, encFileHash, fileHash, mediaKey []byte, fileLength uint64, mediaType, mmsType string, targetPath string) (int64, error)
	DownloadRetriedMediaToFile(ctx context.Context, directPath string, fileHash, mediaKey []byte, fileLength uint64, mediaType, targetPath string) (int64, error)
	SendMediaRetryReceipt(ctx context.Context, info *types.MessageInfo, mediaKey []byte) error

	ParseWebMessage(chatJID types.JID, webMsg *waWeb.WebMessageInfo) (*events.Message, error)
	DecryptReaction(ctx context.Context, reaction *events.Message) (*waProto.ReactionMessage, error)
	SetManualHistorySyncDownload(enabled bool)
	DownloadHistorySync(ctx context.Context, notif *waE2E.HistorySyncNotification) (*waHistorySync.HistorySync, error)
	DeleteHistorySyncMedia(ctx context.Context, notif *waE2E.HistorySyncNotification) error
	RequestHistorySyncOnDemand(ctx context.Context, lastKnown types.MessageInfo, count int) (types.MessageID, error)
	FetchAppState(ctx context.Context, name string, fullSync, onlyIfNotSynced bool) error
	FetchAppStateEvents(ctx context.Context, name string, fullSync, onlyIfNotSynced bool) ([]any, error)
	RequestAppStateRecovery(ctx context.Context, name string) (types.MessageID, error)
	HasLocalAppState(ctx context.Context, name string) (bool, error)
	ContactJournal() *wa.ContactStoreJournal
	LinkedJID() string
	LinkedLID() string

	GetProfilePictureInfo(ctx context.Context, jid types.JID, preview bool, existingID string) (*types.ProfilePictureInfo, error)
	GetBusinessProfile(ctx context.Context, jid types.JID) (*types.BusinessProfile, error)
}

type Options struct {
	StoreDir      string
	Version       string
	JSON          bool
	Events        *out.EventWriter
	AllowUnauthed bool
	ReadOnly      bool
}

type App struct {
	opts                    Options
	waMu                    sync.Mutex
	wa                      WAClient
	sessionState            *sessionObservation
	sessionHandler          uint32
	connectGate             chan struct{}
	sessionResolver         *readOnlySessionResolver
	db                      *store.DB
	statusMu                sync.Mutex
	status                  *syncStatus
	chatStateSync           chan struct{}
	appStatePersist         appStatePersistenceSequencer
	archiveObservationMu    sync.Mutex
	archiveObserved         map[string]int64
	archiveSettingObserved  *events.UnarchiveChatsSetting
	appStateRecoveryWorkers sync.WaitGroup
	appStateRecoveryMu      sync.Mutex
	appStateRecoveryClosing bool
	// Background continuations of recovery attempts the primary answered late,
	// at most one per collection. Guarded by appStateRecoveryMu.
	lateRecoveries   map[string]*lateAppStateRecovery
	lateRecoveryStop chan struct{}
	manualFetchMu    sync.Mutex
	manualFetches    map[string]int
	// contactDrainMu serializes drains of the contact store journal from
	// snapshot to acknowledgement.
	contactDrainMu sync.Mutex
	// Degraded app-state mode: collections whose last full sync is known not
	// to verify (mirrored durably in wacli.db and rehydrated once per
	// process), and the time of the last background recovery request per
	// collection. Guarded by appStateDegradedMu.
	appStateDegradedMu        sync.Mutex
	appStateFullSyncLoaded    bool
	appStateFullSyncBroken    map[string]bool
	appStateRecoveryRequested map[string]time.Time
	heartbeatLast             atomic.Int64
	groupInfo                 groupInfoCache
}

func New(opts Options) (*App, error) {
	if opts.StoreDir == "" {
		return nil, fmt.Errorf("store dir is required")
	}

	indexPath := filepath.Join(opts.StoreDir, "wacli.db")

	var (
		db  *store.DB
		err error
	)
	if opts.ReadOnly {
		db, err = store.OpenReadOnly(indexPath)
	} else {
		if err := fsutil.EnsurePrivateDir(opts.StoreDir); err != nil {
			return nil, fmt.Errorf("create store dir: %w", err)
		}
		db, err = store.Open(indexPath)
	}
	if err != nil {
		return nil, err
	}

	chatStateSync := make(chan struct{}, 1)
	chatStateSync <- struct{}{}
	return &App{opts: opts, db: db, chatStateSync: chatStateSync}, nil
}

func (a *App) OpenWA() error {
	a.waMu.Lock()
	defer a.waMu.Unlock()
	if a.opts.ReadOnly {
		if a.wa != nil {
			return nil
		}
		return fmt.Errorf("read-only mode: command would open the WhatsApp session store")
	}
	if a.wa == nil {
		sessionPath := filepath.Join(a.opts.StoreDir, "session.db")
		cli, err := wa.New(wa.Options{StorePath: sessionPath, KeyStateStore: a.db})
		if err != nil {
			return err
		}
		// Every client goes behind the guard; see guard.go.
		a.wa = guardWAClient(cli)
	}
	if a.sessionState == nil {
		state := newSessionObservation(a.opts.StoreDir)
		a.sessionState = state
		a.sessionHandler = a.wa.AddEventHandler(func(evt any) { a.observeSessionState(state, evt) })
		a.connectGate = make(chan struct{}, 1)
		a.connectGate <- struct{}{}
	}
	return nil
}

func (a *App) Close() {
	a.appStateRecoveryMu.Lock()
	a.appStateRecoveryClosing = true
	if a.lateRecoveryStop != nil {
		close(a.lateRecoveryStop)
		a.lateRecoveryStop = nil
	}
	a.appStateRecoveryMu.Unlock()
	a.waMu.Lock()
	waClient := a.wa
	sessionResolver := a.sessionResolver
	sessionState, sessionHandler := a.sessionState, a.sessionHandler
	a.waMu.Unlock()
	if waClient != nil {
		waClient.Disconnect()
		if sessionState != nil {
			waClient.RemoveEventHandler(sessionHandler)
		}
	}
	// A completed command frontier may hand later ready tasks to a background
	// drainer. Keep SQLite open until that drainer has finished every write.
	a.appStateRecoveryWorkers.Wait()
	_ = a.appStatePersist.waitIdle(context.Background())
	if sessionResolver != nil {
		_ = sessionResolver.Close()
	}
	if waClient != nil {
		waClient.Close()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *App) EnsureAuthed(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.OpenWA(); err != nil {
		return err
	}
	if a.wa.IsAuthed() {
		if a.opts.ReadOnly {
			return nil
		}
		return a.migrateHistoricalLIDs(ctx)
	}
	return fmt.Errorf("not authenticated; run `wacli auth`")
}

func (a *App) WA() WAClient {
	a.waMu.Lock()
	defer a.waMu.Unlock()
	return a.wa
}

type LocalResolver interface {
	ResolveChatName(context.Context, types.JID, string) string
	ResolveLIDToPN(context.Context, types.JID) types.JID
	ResolvePNToLID(context.Context, types.JID) types.JID
}

func (a *App) LocalResolver() (LocalResolver, error) {
	if !a.opts.ReadOnly {
		if err := a.OpenWA(); err != nil {
			return nil, err
		}
		return a.WA(), nil
	}
	return a.ReadOnlyResolver()
}

// ReadOnlyResolver reads session identities without opening or upgrading a client store.
func (a *App) ReadOnlyResolver() (LocalResolver, error) {
	a.waMu.Lock()
	defer a.waMu.Unlock()
	if a.sessionResolver != nil {
		return a.sessionResolver, nil
	}
	resolver, err := openReadOnlySessionResolver(filepath.Join(a.opts.StoreDir, "session.db"))
	if err != nil {
		return nil, err
	}
	a.sessionResolver = resolver
	return resolver, nil
}

func (a *App) DB() *store.DB { return a.db }
func (a *App) Events() *out.EventWriter {
	return a.opts.Events
}
func (a *App) StoreDir() string    { return a.opts.StoreDir }
func (a *App) Version() string     { return a.opts.Version }
func (a *App) AllowUnauthed() bool { return a.opts.AllowUnauthed }
func (a *App) ReadOnly() bool      { return a.opts.ReadOnly }

func (a *App) Connect(ctx context.Context, allowQR bool, qrWriter func(string)) error {
	if a.opts.ReadOnly {
		return fmt.Errorf("read-only mode: command would connect to WhatsApp")
	}
	if err := a.OpenWA(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.connectGate:
	}
	defer func() { a.connectGate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.sessionState.prepareConnect(a.wa.IsConnected); err != nil {
		return err
	}
	if err := a.wa.Connect(ctx, wa.ConnectOptions{
		AllowQR:  allowQR,
		OnQRCode: qrWriter,
	}); err != nil {
		return err
	}
	return a.sessionState.waitForLogin(ctx)
}
