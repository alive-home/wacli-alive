package app

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const contactCollection = appstate.WAPatchCriticalUnblockLow

func contactEvent(jid types.JID, fullName, firstName string) *events.Contact {
	return &events.Contact{JID: jid, Action: &waSyncAction.ContactAction{
		FullName: proto.String(fullName), FirstName: proto.String(firstName),
	}}
}

func contactRecoveryRequired(t *testing.T, a *App) bool {
	t.Helper()
	required, err := a.db.AppStateRecoveryRequired(string(contactCollection))
	if err != nil {
		t.Fatal(err)
	}
	return required
}

func fullSyncFetches(f *fakeWA, name appstate.WAPatchName) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, fetch := range f.appStateFetches {
		if fetch.name == string(name) && fetch.fullSync {
			n++
		}
	}
	return n
}

// whatsmeow's PutContactName can fail (a cancelled context, a busy store)
// while dispatchAppState still emits the event and the collection cursor has
// advanced: the decoded mutation is WhatsApp's state. The intended names must
// reach wacli.db; the stale store row must not be mirrored instead, and the
// event must not be dropped. (Round-2 review; the real-SDK behaviour is
// pinned in internal/wa.)
func TestContactEventSurvivesFailedSessionStoreWrite(t *testing.T) {
	a, f := newContactTestApp(t)
	f.hasLocalAppState = true
	installBoundaryReplayHandler(t, a, f)
	f.applyContactBatch(contactEvent(testContactPN, "Old Name", "Old"))()
	f.mu.Lock()
	f.contactStoreErr[testContactPN] = context.Canceled
	f.mu.Unlock()
	f.applyContactBatch(contactEvent(testContactPN, "Grace Hopper", "Grace"))()
	if info, _ := f.GetContact(t.Context(), testContactPN); info.FullName != "Old Name" {
		t.Fatalf("test setup: the fake store write did not fail: %+v", info)
	}
	if got := storedContact(t, a, testContactPN); got.full != "Grace Hopper" || got.first != "Grace" {
		t.Fatalf("local mirror = %+v after a failed session store write; the decoded mutation says Grace Hopper", got)
	}
	if contactRecoveryRequired(t, a) {
		t.Fatal("debt left for a mutation that was mirrored")
	}
}
