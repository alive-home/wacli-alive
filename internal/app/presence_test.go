package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/out"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestSyncReportsPresenceAndTypingAsEvents(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	pn := types.NewJID("15551234567", types.DefaultUserServer)
	lid := types.NewJID("99887766", types.HiddenUserServer)
	f.lids[lid] = pn
	group := types.NewJID("120363000000000000", types.GroupServer)
	lastSeen := time.Date(2026, 9, 28, 12, 30, 0, 0, time.UTC)

	var eventsOut bytes.Buffer
	a.opts.Events = out.NewEventWriter(&eventsOut, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := a.Sync(ctx, SyncOptions{
		Mode:     SyncModeOnce,
		IdleExit: time.Millisecond,
		AfterConnect: func(context.Context) error {
			f.emit(&events.Presence{From: lid, Unavailable: true, LastSeen: lastSeen})
			f.emit(&events.ChatPresence{
				MessageSource: types.MessageSource{Chat: lid, Sender: lid},
				State:         types.ChatPresenceComposing,
				Media:         types.ChatPresenceMediaAudio,
			})
			f.emit(&events.ChatPresence{
				MessageSource: types.MessageSource{Chat: group, Sender: pn, IsGroup: true},
				State:         types.ChatPresencePaused,
			})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := map[string][]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(eventsOut.String()), "\n") {
		var evt struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		got[evt.Event] = append(got[evt.Event], evt.Data)
	}
	presence := got["presence"]
	if len(presence) != 1 || presence[0]["jid"] != pn.String() || presence[0]["online"] != false || presence[0]["last_seen"] != "2026-09-28T12:30:00Z" {
		t.Fatalf("presence events = %v", presence)
	}
	typing := got["chat_presence"]
	if len(typing) != 2 {
		t.Fatalf("chat_presence events = %v, want 2", typing)
	}
	if typing[0]["chat_jid"] != pn.String() || typing[0]["sender_jid"] != pn.String() || typing[0]["state"] != "composing" || typing[0]["media"] != "audio" {
		t.Fatalf("direct chat typing event = %v", typing[0])
	}
	if typing[1]["chat_jid"] != group.String() || typing[1]["sender_jid"] != pn.String() || typing[1]["state"] != "paused" {
		t.Fatalf("group typing event = %v", typing[1])
	}
}
