package app

import (
	"context"
	"errors"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// errWAMutationBlocked is returned by every WAClient method that would mutate
// WhatsApp state. This build is a read-only fork: sync and read operations are
// allowed, sending and mutating are compiled out at the client boundary.
var errWAMutationBlocked = errors.New("wacli read-only build: mutating WhatsApp is disabled in this binary")

// guardWA wraps a WAClient and hard-blocks every operation that would change
// WhatsApp state visible to others: messages, reactions, polls, edits,
// revokes, read receipts, chat state (read/archive/pin/mute), contact saves,
// group and profile changes, follows, presence subscriptions, and logout.
//
// It deliberately does not embed the wrapped client. Every WAClient method is
// written out below, so a method upstream adds later does not pass through by
// default: the build fails until someone decides here whether it reads or
// mutates. guard_test.go holds the same decision as a list and fails when the
// two disagree.
//
// Read operations and the protocol traffic required for syncing pass through:
// app-state fetches and recovery requests, history-sync requests, downloads,
// and media retry receipts (a media retry receipt only asks the account's own
// phone to upload media again that we could not download; nothing appears in
// any chat).
//
// Presence updates are dropped silently so the session stays passive and never
// announces itself as online or typing. That includes the available presence
// the client would otherwise send right after connecting.
type guardWA struct{ inner WAClient }

var _ WAClient = (*guardWA)(nil)

func guardWAClient(inner WAClient) WAClient { return &guardWA{inner: inner} }

// --- presence: silently dropped -------------------------------------------

func (g *guardWA) SendPresence(ctx context.Context, presence types.Presence) error {
	return nil
}

func (g *guardWA) SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	return nil
}

// --- conditional ----------------------------------------------------------

func (g *guardWA) Connect(ctx context.Context, opts wa.ConnectOptions) error {
	opts.SuppressInitialAvailablePresence = true
	return g.inner.Connect(ctx, opts)
}

func (g *guardWA) ReconnectWithBackoff(ctx context.Context, minDelay, maxDelay time.Duration, opts wa.ConnectOptions) error {
	opts.SuppressInitialAvailablePresence = true
	return g.inner.ReconnectWithBackoff(ctx, minDelay, maxDelay, opts)
}

func (g *guardWA) GetGroupInviteLink(ctx context.Context, group types.JID, reset bool) (string, error) {
	if reset {
		return "", errWAMutationBlocked
	}
	return g.inner.GetGroupInviteLink(ctx, group, reset)
}

func (g *guardWA) Logout(ctx context.Context) error {
	return errors.New("wacli read-only build: this binary cannot remove the device on WhatsApp; remove it on your phone under Linked devices (`wacli auth logout` clears the local session)")
}

// --- blocked: messages ----------------------------------------------------

func (g *guardWA) SendText(ctx context.Context, to types.JID, text string) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SendProtoMessage(ctx context.Context, to types.JID, msg *waProto.Message) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SendProtoMessageWithExtra(ctx context.Context, to types.JID, msg *waProto.Message, mediaHandle string) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SendReaction(ctx context.Context, chat, sender types.JID, targetID types.MessageID, reaction string) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SendPoll(ctx context.Context, to types.JID, name string, options []string, selectable int, ephemeral bool) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SendPollVote(ctx context.Context, pollInfo *types.MessageInfo, options []string) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) RevokeMessage(ctx context.Context, chat types.JID, targetID types.MessageID) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) EditMessage(ctx context.Context, chat types.JID, targetID types.MessageID, text string) (types.MessageID, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) DeleteMessageForMe(ctx context.Context, info types.MessageInfo, deleteMedia bool) error {
	return errWAMutationBlocked
}

// MarkRead sends read receipts: the sender sees blue ticks.
func (g *guardWA) MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, addressing types.AddressingMode) (types.ReceiptType, error) {
	return "", errWAMutationBlocked
}

// --- blocked: chat and account state ----------------------------------------

func (g *guardWA) ArchiveChat(ctx context.Context, target types.JID, archive bool, lastMsgTS time.Time, lastMsgKey *waCommon.MessageKey, beforeApply func()) ([]any, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) PinChat(ctx context.Context, target types.JID, pin bool, beforeApply func()) ([]any, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) MuteChat(ctx context.Context, target types.JID, mute bool, duration time.Duration, beforeApply func()) ([]any, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) MarkChatAsRead(ctx context.Context, target types.JID, read bool, lastMsgTS time.Time, lastMsgKey *waCommon.MessageKey, beforeApply func()) ([]any, error) {
	return nil, errWAMutationBlocked
}

// SendAppStatePatch writes an arbitrary patch to the account (contact saves
// go through it).
func (g *guardWA) SendAppStatePatch(ctx context.Context, patch appstate.PatchInfo, beforeApply func()) ([]any, error) {
	return nil, errWAMutationBlocked
}

// SubscribePresence asks WhatsApp to report when a contact is online. It only
// works for an account that shows itself as online, which this build never
// does.
func (g *guardWA) SubscribePresence(ctx context.Context, jid types.JID) error {
	return errWAMutationBlocked
}

// --- blocked: groups --------------------------------------------------------

func (g *guardWA) CreateGroup(ctx context.Context, req wa.CreateGroupRequest) (*types.GroupInfo, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) SetGroupName(ctx context.Context, jid types.JID, name string) error {
	return errWAMutationBlocked
}

func (g *guardWA) SetGroupTopic(ctx context.Context, jid types.JID, topic string) error {
	return errWAMutationBlocked
}

func (g *guardWA) SetGroupAnnounce(ctx context.Context, jid types.JID, announce bool) error {
	return errWAMutationBlocked
}

func (g *guardWA) SetGroupLocked(ctx context.Context, jid types.JID, locked bool) error {
	return errWAMutationBlocked
}

func (g *guardWA) UpdateGroupParticipants(ctx context.Context, group types.JID, users []types.JID, action wa.GroupParticipantAction) ([]types.GroupParticipant, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) UpdateGroupRequestParticipants(ctx context.Context, group types.JID, users []types.JID, action wa.GroupParticipantRequestAction) ([]types.GroupParticipant, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) JoinGroupWithLink(ctx context.Context, code string) (types.JID, error) {
	return types.JID{}, errWAMutationBlocked
}

func (g *guardWA) LeaveGroup(ctx context.Context, group types.JID) error {
	return errWAMutationBlocked
}

// --- blocked: channels ------------------------------------------------------

func (g *guardWA) FollowNewsletter(ctx context.Context, jid types.JID) error {
	return errWAMutationBlocked
}

func (g *guardWA) UnfollowNewsletter(ctx context.Context, jid types.JID) error {
	return errWAMutationBlocked
}

// --- blocked: profile and media upload --------------------------------------

func (g *guardWA) SetProfilePicture(ctx context.Context, avatar []byte) (string, error) {
	return "", errWAMutationBlocked
}

func (g *guardWA) SetStatusMessage(ctx context.Context, msg string) error {
	return errWAMutationBlocked
}

func (g *guardWA) SetProfileName(ctx context.Context, name string) error {
	return errWAMutationBlocked
}

func (g *guardWA) Upload(ctx context.Context, data []byte, mediaType whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	return whatsmeow.UploadResponse{}, errWAMutationBlocked
}

func (g *guardWA) UploadNewsletter(ctx context.Context, data []byte, mediaType whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	return whatsmeow.UploadResponse{}, errWAMutationBlocked
}

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
