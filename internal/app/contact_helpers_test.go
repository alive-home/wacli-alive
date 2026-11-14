package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

var (
	testContactPN  = types.NewJID("15550001111", types.DefaultUserServer)
	testContactLID = types.NewJID("987654321098765", types.HiddenUserServer)
)

func newContactTestApp(t *testing.T) (*App, *fakeWA) {
	t.Helper()
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	f.lids[testContactLID] = testContactPN
	return a, f
}

// contactPatchInfo is the app-state mutation WhatsApp syncs when another
// device saves a contact; tests encode it to play a server patch back.
func contactPatchInfo(target types.JID, fullName, firstName string) appstate.PatchInfo {
	jid, lid := target.String(), testContactLID.String()
	return appstate.PatchInfo{
		Type: appstate.WAPatchCriticalUnblockLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexContact, jid},
			Version: 2,
			Value: &waSyncAction.SyncActionValue{ContactAction: &waSyncAction.ContactAction{
				FullName:                 &fullName,
				FirstName:                &firstName,
				PnJID:                    &jid,
				LidJID:                   &lid,
				SaveOnPrimaryAddressbook: proto.Bool(false),
			}},
		}},
	}
}

type storedContactNames struct {
	full, first, push, business string
	found                       bool
}

// storedContact reads the contact row after every admitted persistence task.
func storedContact(t *testing.T, a *App, jid types.JID) storedContactNames {
	t.Helper()
	if err := a.appStatePersist.waitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite3", filepath.Join(a.StoreDir(), "wacli.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var full, first, push, business sql.NullString
	err = raw.QueryRow(`SELECT full_name, first_name, push_name, business_name FROM contacts WHERE jid = ?`, jid.String()).
		Scan(&full, &first, &push, &business)
	if errors.Is(err, sql.ErrNoRows) {
		return storedContactNames{}
	}
	if err != nil {
		t.Fatal(err)
	}
	return storedContactNames{full: full.String, first: first.String, push: push.String, business: business.String, found: true}
}
