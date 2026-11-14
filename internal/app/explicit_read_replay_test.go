package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/store"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// Exercise the daemon's production event registration, not only the direct
// store update. The same event persistence code also serves one-shot writes.
func installBoundaryReplayHandler(t *testing.T, a *App, f *fakeWA) {
	t.Helper()
	var stored, last atomic.Int64
	id, _ := a.addSyncEventHandler(t.Context(), SyncOptions{}, &stored, &last,
		make(chan struct{}, 1), make(chan struct{}, 1), make(chan staleReconnectRequest, 1),
		func(string, string) {}, nil, nil, nil)
	t.Cleanup(func() { f.RemoveEventHandler(id) })
}

func boundaryReadEvent(chat types.JID, at time.Time, id string) *events.MarkChatAsRead {
	return &events.MarkChatAsRead{
		JID: chat, Timestamp: at.Add(time.Hour),
		Action: &waSyncAction.MarkChatAsReadAction{
			Read: proto.Bool(true),
			MessageRange: &waSyncAction.SyncActionMessageRange{
				LastMessageTimestamp: proto.Int64(at.Unix()),
				Messages: []*waSyncAction.SyncActionMessage{{
					Key: &waCommon.MessageKey{ID: proto.String(id)}, Timestamp: proto.Int64(at.Unix()),
				}},
			},
		},
	}
}

// seedExplicitReadBoundary stores a chat with three unread messages in the
// same second: their stable row order, rather than the timestamp alone,
// decides which message a read event covers.
func seedExplicitReadBoundary(t *testing.T, a *App) (types.JID, time.Time) {
	t.Helper()
	chat := types.NewJID("456", types.DefaultUserServer)
	at := time.Unix(1700000000, 0)
	if err := a.db.UpsertChat(chat.String(), "dm", "Chat", at); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"earlier", "handled", "newer"} {
		sender := types.NewJID("111", types.DefaultUserServer).String()
		if i == 1 {
			sender = types.NewJID("222", types.DefaultUserServer).String()
		}
		if err := a.db.UpsertMessage(store.UpsertMessageParams{ChatJID: chat.String(), MsgID: id, SenderJID: sender, Timestamp: at, Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.db.SetChatUnreadCount(chat.String(), 3); err != nil {
		t.Fatal(err)
	}
	return chat, at
}

func assertBoundaryMarker(t *testing.T, a *App, chat types.JID, want bool) {
	t.Helper()
	if err := a.appStatePersist.waitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	c, err := a.db.GetChat(chat.String())
	if err != nil || c.Unread != want || c.UnreadCount != 0 {
		t.Fatalf("boundary marker = %+v, %v; want unread=%t count=0", c, err, want)
	}
}

func TestExactReadReplayDoesNotResurrectClearedMarker(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	chat, at := seedExplicitReadBoundary(t, a)
	if err := a.db.SetChatUnreadCount(chat.String(), 0); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetChatUnread(chat.String(), true); err != nil {
		t.Fatal(err)
	}
	installBoundaryReplayHandler(t, a, f)
	// Reading through the newest message on another device clears the marker.
	// A later stale bounded echo must not restore it.
	f.emit(boundaryReadEvent(chat, at, "newer"))
	assertBoundaryMarker(t, a, chat, false)
	evt := boundaryReadEvent(chat, at, "handled")
	evt.FromFullSync = true
	f.emit(evt)
	assertBoundaryMarker(t, a, chat, false)
}

func TestTimestampOnlyReadEventKeepsLegacyMarkerClearing(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	chat, at := seedExplicitReadBoundary(t, a)
	if err := a.db.SetChatUnreadCount(chat.String(), 0); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetChatUnread(chat.String(), true); err != nil {
		t.Fatal(err)
	}
	installBoundaryReplayHandler(t, a, f)
	f.emit(&events.MarkChatAsRead{
		JID: chat, Timestamp: at,
		Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(true)},
	})
	assertBoundaryMarker(t, a, chat, false)
}

func TestExactReadReceiptEventPreservesMarkerWithoutResurrection(t *testing.T) {
	for _, kind := range []types.ReceiptType{types.ReceiptTypeReadSelf, types.ReceiptTypeRead} {
		t.Run(string(kind), func(t *testing.T) {
			a := newTestApp(t)
			f := newFakeWA()
			a.wa = f
			chat, at := seedExplicitReadBoundary(t, a)
			if err := a.db.SetChatUnreadCount(chat.String(), 0); err != nil {
				t.Fatal(err)
			}
			if err := a.db.SetChatUnread(chat.String(), true); err != nil {
				t.Fatal(err)
			}
			installBoundaryReplayHandler(t, a, f)
			receipt := &events.Receipt{MessageSource: types.MessageSource{Chat: chat, IsFromMe: true},
				Type: kind, Timestamp: at.Add(time.Hour), MessageIDs: []types.MessageID{"handled"}}
			f.emit(receipt)
			assertBoundaryMarker(t, a, chat, true)
			f.emit(boundaryReadEvent(chat, at, "newer"))
			assertBoundaryMarker(t, a, chat, false)
			f.emit(receipt)
			assertBoundaryMarker(t, a, chat, false)
		})
	}
}

func TestTimestampOnlyReadReceiptKeepsLegacyMarkerClearing(t *testing.T) {
	for _, ids := range [][]types.MessageID{nil, {""}, {" "}} {
		a := newTestApp(t)
		f := newFakeWA()
		a.wa = f
		chat, at := seedExplicitReadBoundary(t, a)
		if err := a.db.SetChatUnreadCount(chat.String(), 0); err != nil {
			t.Fatal(err)
		}
		if err := a.db.SetChatUnread(chat.String(), true); err != nil {
			t.Fatal(err)
		}
		installBoundaryReplayHandler(t, a, f)
		f.emit(&events.Receipt{MessageSource: types.MessageSource{Chat: chat},
			Type: types.ReceiptTypeReadSelf, Timestamp: at, MessageIDs: ids})
		assertBoundaryMarker(t, a, chat, false)
	}
}
