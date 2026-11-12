package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

func TestGuardWABlocksAllMutations(t *testing.T) {
	g := guardWAClient(&fakeWA{})
	ctx := context.Background()
	jid := types.JID{User: "123", Server: types.DefaultUserServer}

	assertBlocked := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, errWAMutationBlocked) {
			t.Fatalf("%s err = %v, want errWAMutationBlocked", name, err)
		}
	}

	var err error
	_, err = g.SendText(ctx, jid, "hi")
	assertBlocked("SendText", err)
	_, err = g.SendProtoMessage(ctx, jid, nil)
	assertBlocked("SendProtoMessage", err)
	_, err = g.SendProtoMessageWithExtra(ctx, jid, nil, "")
	assertBlocked("SendProtoMessageWithExtra", err)
	_, err = g.SendReaction(ctx, jid, jid, "mid", "👍")
	assertBlocked("SendReaction", err)
	_, err = g.SendPoll(ctx, jid, "q", []string{"a"}, 1, false)
	assertBlocked("SendPoll", err)
	_, err = g.SendPollVote(ctx, nil, []string{"a"})
	assertBlocked("SendPollVote", err)
	_, err = g.RevokeMessage(ctx, jid, "mid")
	assertBlocked("RevokeMessage", err)
	_, err = g.EditMessage(ctx, jid, "mid", "x")
	assertBlocked("EditMessage", err)
	assertBlocked("DeleteMessageForMe", g.DeleteMessageForMe(ctx, types.MessageInfo{}, false))

	_, err = g.ArchiveChat(ctx, jid, true, time.Now(), nil, nil)
	assertBlocked("ArchiveChat", err)
	_, err = g.PinChat(ctx, jid, true, nil)
	assertBlocked("PinChat", err)
	_, err = g.MuteChat(ctx, jid, true, time.Hour, nil)
	assertBlocked("MuteChat", err)
	_, err = g.MarkChatAsRead(ctx, jid, true, time.Now(), nil, nil)
	assertBlocked("MarkChatAsRead", err)

	_, err = g.CreateGroup(ctx, wa.CreateGroupRequest{})
	assertBlocked("CreateGroup", err)
	assertBlocked("SetGroupName", g.SetGroupName(ctx, jid, "n"))
	assertBlocked("SetGroupTopic", g.SetGroupTopic(ctx, jid, "t"))
	assertBlocked("SetGroupAnnounce", g.SetGroupAnnounce(ctx, jid, true))
	assertBlocked("SetGroupLocked", g.SetGroupLocked(ctx, jid, true))
	_, err = g.UpdateGroupParticipants(ctx, jid, nil, wa.GroupParticipantAdd)
	assertBlocked("UpdateGroupParticipants", err)
	_, err = g.UpdateGroupRequestParticipants(ctx, jid, nil, wa.GroupParticipantRequestApprove)
	assertBlocked("UpdateGroupRequestParticipants", err)
	_, err = g.JoinGroupWithLink(ctx, "code")
	assertBlocked("JoinGroupWithLink", err)
	assertBlocked("LeaveGroup", g.LeaveGroup(ctx, jid))

	assertBlocked("FollowNewsletter", g.FollowNewsletter(ctx, jid))
	assertBlocked("UnfollowNewsletter", g.UnfollowNewsletter(ctx, jid))

	_, err = g.SetProfilePicture(ctx, []byte("img"))
	assertBlocked("SetProfilePicture", err)
	assertBlocked("SetStatusMessage", g.SetStatusMessage(ctx, "s"))
	assertBlocked("SetProfileName", g.SetProfileName(ctx, "n"))

	_, err = g.Upload(ctx, []byte("data"), "image")
	assertBlocked("Upload", err)
	_, err = g.UploadNewsletter(ctx, []byte("data"), "image")
	assertBlocked("UploadNewsletter", err)

	_, err = g.GetGroupInviteLink(ctx, jid, true)
	assertBlocked("GetGroupInviteLink reset", err)

	if err := g.Logout(ctx); err == nil {
		t.Fatal("Logout err = nil, want blocked")
	}
}

func TestGuardWADropsPresenceSilently(t *testing.T) {
	g := guardWAClient(&fakeWA{})
	ctx := context.Background()
	if err := g.SendPresence(ctx, types.PresenceAvailable); err != nil {
		t.Fatalf("SendPresence err = %v, want nil", err)
	}
	if err := g.SendChatPresence(ctx, types.JID{}, types.ChatPresenceComposing, types.ChatPresenceMediaText); err != nil {
		t.Fatalf("SendChatPresence err = %v, want nil", err)
	}
}

func TestGuardWAPassesReadsThrough(t *testing.T) {
	inner := &fakeWA{authed: true}
	g := guardWAClient(inner)
	ctx := context.Background()

	if !g.IsAuthed() {
		t.Fatal("IsAuthed = false, want true (passthrough)")
	}
	if _, err := g.GetJoinedGroups(ctx); err != nil {
		t.Fatalf("GetJoinedGroups err = %v, want nil", err)
	}
	if _, err := g.GetGroupInviteLink(ctx, types.JID{}, false); errors.Is(err, errWAMutationBlocked) {
		t.Fatal("GetGroupInviteLink(reset=false) blocked, want passthrough")
	}
}
