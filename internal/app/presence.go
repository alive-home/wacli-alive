package app

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// PresenceState is what WhatsApp said about a user's presence.
type PresenceState struct {
	JID    string `json:"jid"`
	Online bool   `json:"online"`
	// LastSeen is empty while the user is online, and when WhatsApp withholds
	// it (the user hides it, or this account hides its own).
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

func presenceStateFromEvent(jid types.JID, evt *events.Presence) PresenceState {
	state := PresenceState{JID: jid.String(), Online: !evt.Unavailable}
	if !evt.LastSeen.IsZero() {
		lastSeen := evt.LastSeen.UTC()
		state.LastSeen = &lastSeen
	}
	return state
}

// presenceEventData is a presence update as the sync reports it with --events,
// under the JID the store uses for the user's chat.
func (a *App) presenceEventData(ctx context.Context, evt *events.Presence) map[string]any {
	state := presenceStateFromEvent(a.canonicalStoreJID(ctx, evt.From), evt)
	data := map[string]any{"jid": state.JID, "online": state.Online}
	if state.LastSeen != nil {
		data["last_seen"] = state.LastSeen.Format(time.RFC3339)
	}
	return data
}

// chatPresenceEventData is a typing update as the sync reports it with
// --events: "composing" while someone types (media "audio" while they record a
// voice note) and "paused" when they stop.
func (a *App) chatPresenceEventData(ctx context.Context, evt *events.ChatPresence) (map[string]any, bool) {
	if evt == nil || evt.Chat.IsEmpty() {
		return nil, false
	}
	if evt.State != types.ChatPresenceComposing && evt.State != types.ChatPresencePaused {
		return nil, false
	}
	data := map[string]any{
		"chat_jid": a.canonicalStoreJID(ctx, evt.Chat).String(),
		"state":    string(evt.State),
		"media":    string(evt.Media),
	}
	if !evt.Sender.IsEmpty() {
		data["sender_jid"] = a.canonicalStoreJID(ctx, evt.Sender).String()
	}
	return data, true
}
