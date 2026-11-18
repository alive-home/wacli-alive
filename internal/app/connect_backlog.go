package app

import (
	"context"
	"sync/atomic"

	"go.mau.fi/whatsmeow/types/events"
)

// keepBacklog stores what WhatsApp delivers while a command other than sync is
// connected (media download, contacts check, groups info, doctor --connect).
//
// WhatsApp hands a connecting device everything it missed and forgets each item
// once it is acknowledged, and whatsmeow acknowledges whether or not anyone
// stored it. Without this such a command ate the backlog: on 16 November 2026
// one media download lost 45 minutes of chats for every sync after it.
func (a *App) keepBacklog(ctx context.Context) {
	a.waMu.Lock()
	defer a.waMu.Unlock()
	if a.backlogKept || a.wa == nil {
		return
	}
	var stored, lastEvent atomic.Int64
	handle, _ := a.newSyncEventHandler(ctx, SyncOptions{Mode: SyncModeOnce}, &stored, &lastEvent, nil, nil, nil, func(string, string) {}, nil, nil, nil)
	a.backlogHandler = a.wa.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.Connected, *events.Disconnected:
			// The command reports its own result; sync's connection lines
			// would only be noise around it.
			return
		}
		handle(evt)
	})
	a.backlogKept = true
}

// dropBacklogKeeper hands the events back: to sync, which registers the same
// handler with its own queues, or to nobody when the app closes.
func (a *App) dropBacklogKeeper() {
	a.waMu.Lock()
	defer a.waMu.Unlock()
	if !a.backlogKept || a.wa == nil {
		return
	}
	a.wa.RemoveEventHandler(a.backlogHandler)
	a.backlogKept = false
}
