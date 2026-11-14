package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/openclaw/wacli/internal/store"
	"go.mau.fi/whatsmeow/types"
)

// drainContactJournal projects names (including clears and failed SDK writes)
// under canonical JIDs in one transaction. Drains serialize through Ack so
// PN/LID aliases keep application order. Failure acknowledges nothing.
func (a *App) drainContactJournal(ctx context.Context) error {
	journal := a.wa.ContactJournal()
	a.contactDrainMu.Lock()
	defer a.contactDrainMu.Unlock()
	if journal == nil {
		return nil
	}
	writes, _ := journal.Snapshot()
	if len(writes) == 0 {
		return nil
	}
	names := make([]store.ContactActionName, 0, len(writes))
	for _, write := range writes {
		if write.JID.IsEmpty() {
			continue
		}
		contact := a.canonicalStoreJID(ctx, write.JID)
		if contact.Server != types.DefaultUserServer && contact.Server != types.HiddenUserServer {
			continue
		}
		phone := ""
		if contact.Server == types.DefaultUserServer {
			phone = contact.User
		}
		names = append(names, store.ContactActionName{
			JID: contact.String(), Phone: phone,
			FullName: strings.TrimSpace(write.FullName), FirstName: strings.TrimSpace(write.FirstName),
		})
	}
	if err := a.db.SetContactActionNames(names); err != nil {
		a.emitWarning(
			"contact_store_failed",
			fmt.Sprintf("warning: failed to store %d contact names from the session: %v", len(names), err),
			map[string]any{"count": len(names), "error": err.Error()},
		)
		return fmt.Errorf("mirror %d contact writes: %w", len(names), err)
	}
	journal.Ack(writes[len(writes)-1].Seq)
	return nil
}
