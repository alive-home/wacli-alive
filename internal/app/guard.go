package app

import (
	"context"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// guardWA is the only way the app reaches the WhatsApp client. This build
// cannot send to or change WhatsApp: WAClient and wa.Client have no method
// that does, and the guard keeps it that way.
//
// It deliberately does not embed the wrapped client. Every WAClient method is
// written out below, so a method added to the interface does not pass through
// by default: the build fails until someone reads what it does on the wire and
// writes it here. Only add a method that reads, decodes locally, or carries
// the protocol traffic syncing needs: app-state fetches and recovery requests,
// history-sync requests, downloads, and media retry receipts (a media retry
// receipt only asks the account's own phone to upload media again that we
// could not download; nothing appears in any chat). guard_test.go holds the
// same list, and the names that must never come back.
type guardWA struct{ inner WAClient }

var _ WAClient = (*guardWA)(nil)

func guardWAClient(inner WAClient) WAClient { return &guardWA{inner: inner} }

// --- passed through: connection ---------------------------------------------

func (g *guardWA) Close()            { g.inner.Close() }
func (g *guardWA) Disconnect()       { g.inner.Disconnect() }
func (g *guardWA) IsAuthed() bool    { return g.inner.IsAuthed() }
func (g *guardWA) IsConnected() bool { return g.inner.IsConnected() }
func (g *guardWA) LinkedJID() string { return g.inner.LinkedJID() }
func (g *guardWA) LinkedLID() string { return g.inner.LinkedLID() }

func (g *guardWA) SetAutoReconnect(enabled bool) (bool, bool) {
	return g.inner.SetAutoReconnect(enabled)
}

func (g *guardWA) Connect(ctx context.Context, opts wa.ConnectOptions) error {
	return g.inner.Connect(ctx, opts)
}

func (g *guardWA) ReconnectWithBackoff(ctx context.Context, minDelay, maxDelay time.Duration, opts wa.ConnectOptions) error {
	return g.inner.ReconnectWithBackoff(ctx, minDelay, maxDelay, opts)
}

func (g *guardWA) AddEventHandler(handler func(any)) uint32 { return g.inner.AddEventHandler(handler) }
func (g *guardWA) RemoveEventHandler(id uint32)             { g.inner.RemoveEventHandler(id) }

// --- passed through: contacts, groups and channels (reads) -------------------

func (g *guardWA) ResolveChatName(ctx context.Context, chat types.JID, pushName string) string {
	return g.inner.ResolveChatName(ctx, chat, pushName)
}

func (g *guardWA) ResolveLIDToPN(ctx context.Context, jid types.JID) types.JID {
	return g.inner.ResolveLIDToPN(ctx, jid)
}

func (g *guardWA) ResolvePNToLID(ctx context.Context, jid types.JID) types.JID {
	return g.inner.ResolvePNToLID(ctx, jid)
}

func (g *guardWA) GetUserInfo(ctx context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error) {
	return g.inner.GetUserInfo(ctx, jids)
}

func (g *guardWA) IsOnWhatsApp(ctx context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
	return g.inner.IsOnWhatsApp(ctx, phones)
}

func (g *guardWA) GetContact(ctx context.Context, jid types.JID) (types.ContactInfo, error) {
	return g.inner.GetContact(ctx, jid)
}

func (g *guardWA) GetAllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return g.inner.GetAllContacts(ctx)
}

func (g *guardWA) ContactJournal() *wa.ContactStoreJournal { return g.inner.ContactJournal() }

func (g *guardWA) GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error) {
	return g.inner.GetJoinedGroups(ctx)
}

func (g *guardWA) GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error) {
	return g.inner.GetGroupInfo(ctx, jid)
}

func (g *guardWA) GetGroupRequestParticipants(ctx context.Context, group types.JID) ([]types.GroupParticipantRequest, error) {
	return g.inner.GetGroupRequestParticipants(ctx, group)
}

func (g *guardWA) GetGroupInviteLink(ctx context.Context, group types.JID) (string, error) {
	return g.inner.GetGroupInviteLink(ctx, group)
}

func (g *guardWA) GetNewsletterInfoWithInvite(ctx context.Context, key string) (*types.NewsletterMetadata, error) {
	return g.inner.GetNewsletterInfoWithInvite(ctx, key)
}

func (g *guardWA) GetSubscribedNewsletters(ctx context.Context) ([]*types.NewsletterMetadata, error) {
	return g.inner.GetSubscribedNewsletters(ctx)
}

func (g *guardWA) GetNewsletterInfo(ctx context.Context, jid types.JID) (*types.NewsletterMetadata, error) {
	return g.inner.GetNewsletterInfo(ctx, jid)
}

func (g *guardWA) GetProfilePictureInfo(ctx context.Context, jid types.JID, preview bool, existingID string) (*types.ProfilePictureInfo, error) {
	return g.inner.GetProfilePictureInfo(ctx, jid, preview, existingID)
}

func (g *guardWA) GetBusinessProfile(ctx context.Context, jid types.JID) (*types.BusinessProfile, error) {
	return g.inner.GetBusinessProfile(ctx, jid)
}

// --- passed through: decoding received messages (local) ----------------------

func (g *guardWA) DecryptPollVote(ctx context.Context, evt *events.Message) (*waE2E.PollVoteMessage, error) {
	return g.inner.DecryptPollVote(ctx, evt)
}

func (g *guardWA) DecryptSecretEncryptedMessage(ctx context.Context, evt *events.Message) (*waE2E.Message, error) {
	return g.inner.DecryptSecretEncryptedMessage(ctx, evt)
}

func (g *guardWA) DecryptReaction(ctx context.Context, reaction *events.Message) (*waProto.ReactionMessage, error) {
	return g.inner.DecryptReaction(ctx, reaction)
}

func (g *guardWA) ParseWebMessage(chatJID types.JID, webMsg *waWeb.WebMessageInfo) (*events.Message, error) {
	return g.inner.ParseWebMessage(chatJID, webMsg)
}

// --- passed through: media download -----------------------------------------

func (g *guardWA) DownloadMediaToFile(ctx context.Context, directPath string, encFileHash, fileHash, mediaKey []byte, fileLength uint64, mediaType, mmsType string, targetPath string) (int64, error) {
	return g.inner.DownloadMediaToFile(ctx, directPath, encFileHash, fileHash, mediaKey, fileLength, mediaType, mmsType, targetPath)
}

func (g *guardWA) DownloadRetriedMediaToFile(ctx context.Context, directPath string, fileHash, mediaKey []byte, fileLength uint64, mediaType, targetPath string) (int64, error) {
	return g.inner.DownloadRetriedMediaToFile(ctx, directPath, fileHash, mediaKey, fileLength, mediaType, targetPath)
}

func (g *guardWA) SendMediaRetryReceipt(ctx context.Context, info *types.MessageInfo, mediaKey []byte) error {
	return g.inner.SendMediaRetryReceipt(ctx, info, mediaKey)
}

// --- passed through: history and app-state sync -------------------------------
//
// These talk to WhatsApp's servers or to the account's own primary device.
// None of them is visible in a chat or to another person.

func (g *guardWA) SetManualHistorySyncDownload(enabled bool) {
	g.inner.SetManualHistorySyncDownload(enabled)
}

func (g *guardWA) DownloadHistorySync(ctx context.Context, notif *waE2E.HistorySyncNotification) (*waHistorySync.HistorySync, error) {
	return g.inner.DownloadHistorySync(ctx, notif)
}

// DeleteHistorySyncMedia removes the history bundle the primary uploaded for
// this device once it is stored locally.
func (g *guardWA) DeleteHistorySyncMedia(ctx context.Context, notif *waE2E.HistorySyncNotification) error {
	return g.inner.DeleteHistorySyncMedia(ctx, notif)
}

func (g *guardWA) RequestHistorySyncOnDemand(ctx context.Context, lastKnown types.MessageInfo, count int) (types.MessageID, error) {
	return g.inner.RequestHistorySyncOnDemand(ctx, lastKnown, count)
}

func (g *guardWA) FetchAppState(ctx context.Context, name string, fullSync, onlyIfNotSynced bool) error {
	return g.inner.FetchAppState(ctx, name, fullSync, onlyIfNotSynced)
}

func (g *guardWA) FetchAppStateEvents(ctx context.Context, name string, fullSync, onlyIfNotSynced bool) ([]any, error) {
	return g.inner.FetchAppStateEvents(ctx, name, fullSync, onlyIfNotSynced)
}

func (g *guardWA) RequestAppStateRecovery(ctx context.Context, name string) (types.MessageID, error) {
	return g.inner.RequestAppStateRecovery(ctx, name)
}

func (g *guardWA) HasLocalAppState(ctx context.Context, name string) (bool, error) {
	return g.inner.HasLocalAppState(ctx, name)
}
