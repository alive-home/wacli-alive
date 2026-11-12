package app

import (
	"context"
	"errors"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
)

// errWAMutationBlocked is returned by every WAClient method that would mutate
// WhatsApp state. This build is a read-only fork: sync and read operations are
// allowed, sending and mutating are compiled out at the client boundary.
var errWAMutationBlocked = errors.New("wacli read-only build: mutating WhatsApp is disabled in this binary")

// guardWA wraps a WAClient and hard-blocks every operation that would change
// WhatsApp state visible to others: messages, reactions, polls, edits,
// revokes, chat state (read/archive/pin/mute), group and profile changes,
// follows, and logout.
//
// Read operations and the protocol traffic required for syncing pass through:
// app-state fetches and recovery requests, history-sync requests, downloads,
// and media retry receipts (a retry receipt only asks the sender's devices to
// re-encrypt media we could not download; nothing appears in any chat).
//
// Presence updates are dropped silently so the session stays passive and never
// announces itself as online or typing.
type guardWA struct{ WAClient }

var _ WAClient = (*guardWA)(nil)

func guardWAClient(inner WAClient) WAClient { return &guardWA{inner} }

// --- presence: silently dropped -------------------------------------------

func (g *guardWA) SendPresence(ctx context.Context, presence types.Presence) error {
	return nil
}

func (g *guardWA) SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	return nil
}

// --- conditional ----------------------------------------------------------

func (g *guardWA) GetGroupInviteLink(ctx context.Context, group types.JID, reset bool) (string, error) {
	if reset {
		return "", errWAMutationBlocked
	}
	return g.WAClient.GetGroupInviteLink(ctx, group, reset)
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

// --- blocked: chat state ----------------------------------------------------

func (g *guardWA) ArchiveChat(ctx context.Context, target types.JID, archive bool, lastMsgTS time.Time, lastMsgKey *waCommon.MessageKey, beforeApply func()) ([]interface{}, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) PinChat(ctx context.Context, target types.JID, pin bool, beforeApply func()) ([]interface{}, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) MuteChat(ctx context.Context, target types.JID, mute bool, duration time.Duration, beforeApply func()) ([]interface{}, error) {
	return nil, errWAMutationBlocked
}

func (g *guardWA) MarkChatAsRead(ctx context.Context, target types.JID, read bool, lastMsgTS time.Time, lastMsgKey *waCommon.MessageKey, beforeApply func()) ([]interface{}, error) {
	return nil, errWAMutationBlocked
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

// --- blocked: profile / media upload / session ------------------------------

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

func (g *guardWA) Logout(ctx context.Context) error {
	return errors.New("wacli read-only build: logout is disabled; unlink this device from WhatsApp on your phone (Linked devices)")
}
