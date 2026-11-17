package app

import (
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// 16 November 2026: a `media download` connected, WhatsApp replayed the 45
// minutes this device had missed, whatsmeow acknowledged them, and nothing
// stored them. No later sync ever got those chats again.
func TestConnectOutsideSyncStoresWhatArrives(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f

	chat := types.JID{User: "123", Server: types.DefaultUserServer}
	f.connectEvents = []any{&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            "m-backlog",
			Timestamp:     time.Date(2026, 11, 16, 20, 8, 20, 0, time.UTC),
		},
		Message: &waProto.Message{Conversation: proto.String("missed while offline")},
	}}

	if err := a.Connect(t.Context(), false, nil); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	msg, err := a.db.GetMessage(chat.String(), "m-backlog")
	if err != nil {
		t.Fatalf("the message that arrived on connect was not stored: %v", err)
	}
	if msg.Text != "missed while offline" {
		t.Fatalf("stored text = %q", msg.Text)
	}

	// A sync in the same process takes the events over; it must not find the
	// keeper still registered, or every message would be handled twice.
	if _, err := a.Sync(t.Context(), SyncOptions{Mode: SyncModeOnce, IdleExit: time.Millisecond}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	a.Close()
	if len(f.handlers) != 0 {
		t.Fatalf("Close leaked %d event handlers", len(f.handlers))
	}
}
