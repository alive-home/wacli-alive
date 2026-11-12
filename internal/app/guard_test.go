package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

// The read-only decision for every WAClient method. A method upstream adds
// must be put in one of these lists (and written out in guard.go) before the
// tests pass; when in doubt it belongs in guardBlocked.

// guardBlocked methods return errWAMutationBlocked without reaching WhatsApp.
var guardBlocked = []string{
	"SendText", "SendProtoMessage", "SendProtoMessageWithExtra", "SendReaction",
	"SendPoll", "SendPollVote", "RevokeMessage", "EditMessage", "DeleteMessageForMe",
	"MarkRead",
	"ArchiveChat", "PinChat", "MuteChat", "MarkChatAsRead", "SendAppStatePatch",
	"SubscribePresence",
	"CreateGroup", "SetGroupName", "SetGroupTopic", "SetGroupAnnounce", "SetGroupLocked",
	"UpdateGroupParticipants", "UpdateGroupRequestParticipants", "JoinGroupWithLink", "LeaveGroup",
	"FollowNewsletter", "UnfollowNewsletter",
	"SetProfilePicture", "SetStatusMessage", "SetProfileName",
	"Upload", "UploadNewsletter",
}

// guardDropped methods report success without reaching WhatsApp.
var guardDropped = []string{"SendPresence", "SendChatPresence"}

// guardPassThrough methods read, decode locally, or carry the protocol
// traffic syncing needs.
var guardPassThrough = []string{
	"Close", "Disconnect", "IsAuthed", "IsConnected", "LinkedJID", "LinkedLID",
	"SetAutoReconnect", "AddEventHandler", "RemoveEventHandler",
	"ResolveChatName", "ResolveLIDToPN", "ResolvePNToLID", "GetUserInfo", "IsOnWhatsApp",
	"GetContact", "GetAllContacts", "ContactJournal",
	"GetJoinedGroups", "GetGroupInfo", "GetGroupRequestParticipants",
	"GetNewsletterInfoWithInvite", "GetSubscribedNewsletters", "GetNewsletterInfo",
	"GetProfilePictureInfo", "GetBusinessProfile",
	"DecryptPollVote", "DecryptSecretEncryptedMessage", "DecryptReaction", "ParseWebMessage",
	"DownloadMediaToFile", "DownloadRetriedMediaToFile", "SendMediaRetryReceipt",
	"SetManualHistorySyncDownload", "DownloadHistorySync", "DeleteHistorySyncMedia",
	"RequestHistorySyncOnDemand", "FetchAppState", "FetchAppStateEvents",
	"RequestAppStateRecovery", "HasLocalAppState",
}

// guardSpecial methods have their own test below.
var guardSpecial = []string{"Connect", "ReconnectWithBackoff", "GetGroupInviteLink", "Logout"}

var waClientType = reflect.TypeOf((*WAClient)(nil)).Elem()

func TestGuardWAClassifiesEveryMethod(t *testing.T) {
	seen := map[string]int{}
	for _, list := range [][]string{guardBlocked, guardDropped, guardPassThrough, guardSpecial} {
		for _, name := range list {
			seen[name]++
		}
	}
	for i := 0; i < waClientType.NumMethod(); i++ {
		name := waClientType.Method(i).Name
		if seen[name] != 1 {
			t.Errorf("WAClient.%s is in %d guard lists, want exactly 1", name, seen[name])
		}
		delete(seen, name)
	}
	for name := range seen {
		t.Errorf("guard lists name %s, which WAClient does not have", name)
	}
}

// callOnBareGuard calls a method on a guard that wraps nothing, so a method
// that reaches for the wrapped client panics.
func callOnBareGuard(t *testing.T, name string) (results []reflect.Value, reachedInner bool) {
	t.Helper()
	method := reflect.ValueOf(&guardWA{}).MethodByName(name)
	if !method.IsValid() {
		t.Fatalf("guardWA has no method %s", name)
	}
	args := make([]reflect.Value, method.Type().NumIn())
	for i := range args {
		args[i] = reflect.Zero(method.Type().In(i))
	}
	defer func() {
		if recover() != nil {
			reachedInner = true
		}
	}()
	return method.Call(args), false
}

func lastError(results []reflect.Value) error {
	err, _ := results[len(results)-1].Interface().(error)
	return err
}

func TestGuardWABlocksEveryMutation(t *testing.T) {
	for _, name := range guardBlocked {
		results, reachedInner := callOnBareGuard(t, name)
		if reachedInner {
			t.Errorf("%s reached the WhatsApp client, want blocked", name)
			continue
		}
		if err := lastError(results); !errors.Is(err, errWAMutationBlocked) {
			t.Errorf("%s err = %v, want errWAMutationBlocked", name, err)
		}
	}
}

func TestGuardWADropsPresenceSilently(t *testing.T) {
	for _, name := range guardDropped {
		results, reachedInner := callOnBareGuard(t, name)
		if reachedInner {
			t.Errorf("%s reached the WhatsApp client, want dropped", name)
			continue
		}
		if err := lastError(results); err != nil {
			t.Errorf("%s err = %v, want nil", name, err)
		}
	}
}

func TestGuardWAPassesReadsThrough(t *testing.T) {
	for _, name := range guardPassThrough {
		if _, reachedInner := callOnBareGuard(t, name); !reachedInner {
			t.Errorf("%s did not reach the WhatsApp client, want passthrough", name)
		}
	}

	g := guardWAClient(&fakeWA{authed: true})
	if !g.IsAuthed() {
		t.Fatal("IsAuthed = false, want true (passthrough)")
	}
	if _, err := g.GetJoinedGroups(context.Background()); err != nil {
		t.Fatalf("GetJoinedGroups err = %v, want nil", err)
	}
}

func TestGuardWALogoutIsDisabled(t *testing.T) {
	results, reachedInner := callOnBareGuard(t, "Logout")
	if reachedInner {
		t.Fatal("Logout reached the WhatsApp client, want disabled")
	}
	if lastError(results) == nil {
		t.Fatal("Logout err = nil, want disabled")
	}
}

func TestGuardWAInviteLinkReadsButNeverResets(t *testing.T) {
	jid := types.JID{User: "123", Server: types.GroupServer}
	if _, err := (&guardWA{}).GetGroupInviteLink(context.Background(), jid, true); !errors.Is(err, errWAMutationBlocked) {
		t.Fatalf("GetGroupInviteLink(reset=true) err = %v, want errWAMutationBlocked", err)
	}
	g := guardWAClient(&fakeWA{})
	if _, err := g.GetGroupInviteLink(context.Background(), jid, false); errors.Is(err, errWAMutationBlocked) {
		t.Fatal("GetGroupInviteLink(reset=false) blocked, want passthrough")
	}
}

// connectRecorder records the options the guard hands to the wrapped client.
type connectRecorder struct {
	WAClient
	opts []wa.ConnectOptions
}

func (c *connectRecorder) Connect(ctx context.Context, opts wa.ConnectOptions) error {
	c.opts = append(c.opts, opts)
	return nil
}

func (c *connectRecorder) ReconnectWithBackoff(ctx context.Context, minDelay, maxDelay time.Duration, opts wa.ConnectOptions) error {
	c.opts = append(c.opts, opts)
	return nil
}

func TestGuardWANeverAnnouncesOnlineOnConnect(t *testing.T) {
	rec := &connectRecorder{}
	g := guardWAClient(rec)
	if err := g.Connect(context.Background(), wa.ConnectOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := g.ReconnectWithBackoff(context.Background(), time.Second, time.Second, wa.ConnectOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(rec.opts) != 2 || slices.ContainsFunc(rec.opts, func(o wa.ConnectOptions) bool { return !o.SuppressInitialAvailablePresence }) {
		t.Fatalf("connect options = %+v, want the available presence suppressed on both", rec.opts)
	}
}

// Every client the app opens itself is the guard, never the bare client.
func TestOpenWAWrapsTheClientInTheGuard(t *testing.T) {
	a, err := New(Options{StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.OpenWA(); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.wa.(*guardWA); !ok {
		t.Fatalf("OpenWA client is %T, want *guardWA", a.wa)
	}
}
