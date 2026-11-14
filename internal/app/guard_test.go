package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/openclaw/wacli/internal/wa"
)

// Every WAClient method, as guard.go writes them out. A method upstream adds
// to the interface must be written out there and listed here before the tests
// pass, and only if it reads, decodes locally, or carries the protocol traffic
// syncing needs.
var guardPassThrough = []string{
	"Close", "Disconnect", "IsAuthed", "IsConnected", "LinkedJID", "LinkedLID",
	"SetAutoReconnect", "Connect", "ReconnectWithBackoff", "AddEventHandler", "RemoveEventHandler",
	"ResolveChatName", "ResolveLIDToPN", "ResolvePNToLID", "GetUserInfo", "IsOnWhatsApp",
	"GetContact", "GetAllContacts", "ContactJournal",
	"GetJoinedGroups", "GetGroupInfo", "GetGroupInviteLink", "GetGroupRequestParticipants",
	"GetNewsletterInfoWithInvite", "GetSubscribedNewsletters", "GetNewsletterInfo",
	"GetProfilePictureInfo", "GetBusinessProfile",
	"DecryptPollVote", "DecryptSecretEncryptedMessage", "DecryptReaction", "ParseWebMessage",
	"DownloadMediaToFile", "DownloadRetriedMediaToFile", "SendMediaRetryReceipt",
	"SetManualHistorySyncDownload", "DownloadHistorySync", "DeleteHistorySyncMedia",
	"RequestHistorySyncOnDemand", "FetchAppState", "FetchAppStateEvents",
	"RequestAppStateRecovery", "HasLocalAppState",
}

// removedMutations are the methods upstream's client has for sending to and
// changing WhatsApp. This build removed them; none may come back, neither on
// WAClient nor on the client underneath it.
var removedMutations = []string{
	"SendText", "SendProtoMessage", "SendProtoMessageWithExtra", "SendReaction",
	"SendPoll", "SendPollVote", "RevokeMessage", "EditMessage", "DeleteMessageForMe",
	"MarkRead",
	"ArchiveChat", "PinChat", "MuteChat", "MarkChatAsRead", "SendAppState", "SendAppStatePatch",
	"SendPresence", "SendChatPresence", "SubscribePresence",
	"CreateGroup", "SetGroupName", "SetGroupTopic", "SetGroupAnnounce", "SetGroupLocked",
	"UpdateGroupParticipants", "UpdateGroupRequestParticipants", "JoinGroupWithLink", "LeaveGroup",
	"FollowNewsletter", "UnfollowNewsletter",
	"SetProfilePicture", "SetStatusMessage", "SetProfileName",
	"Upload", "UploadNewsletter",
	"Logout",
}

var waClientType = reflect.TypeOf((*WAClient)(nil)).Elem()

func TestGuardWAClassifiesEveryMethod(t *testing.T) {
	seen := map[string]int{}
	for _, name := range guardPassThrough {
		seen[name]++
	}
	for i := 0; i < waClientType.NumMethod(); i++ {
		name := waClientType.Method(i).Name
		if seen[name] != 1 {
			t.Errorf("WAClient.%s is listed %d times in guardPassThrough, want once", name, seen[name])
		}
		delete(seen, name)
	}
	for name := range seen {
		t.Errorf("guardPassThrough names %s, which WAClient does not have", name)
	}
}

// Nothing in this build can send to or change WhatsApp: the methods are gone
// from the interface and from the client, and the client has no exported
// method the interface (and so the guard and its list) does not know.
func TestNoMutatingMethodExists(t *testing.T) {
	clientType := reflect.TypeOf(&wa.Client{})
	for _, name := range removedMutations {
		if _, ok := waClientType.MethodByName(name); ok {
			t.Errorf("WAClient has %s, which sends to or changes WhatsApp", name)
		}
		if _, ok := clientType.MethodByName(name); ok {
			t.Errorf("wa.Client has %s, which sends to or changes WhatsApp", name)
		}
	}
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		if _, ok := waClientType.MethodByName(name); !ok {
			t.Errorf("wa.Client.%s is not a WAClient method, so nothing classifies it", name)
		}
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
