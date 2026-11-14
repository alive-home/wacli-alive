package app

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/appstate"
)

// recoveryIntentExists reports whether one exact durable recovery intent is
// still present in wacli.db.
func recoveryIntentExists(t *testing.T, a *App, name string, id int64) bool {
	t.Helper()
	raw, err := sql.Open("sqlite3", filepath.Join(a.StoreDir(), "wacli.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var exists bool
	if err := raw.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM app_state_recovery_intents WHERE collection = ? AND id = ?)`, name, id,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

// A full sync that does not verify flags its collection, and the flag is
// durable: the next process must know it before it tries the same full sync.
func TestFullSyncBrokenFlagPersistsAcrossRestart(t *testing.T) {
	a := newTestApp(t)
	f := &appStateContextWA{fakeWA: newFakeWA()}
	a.wa = f
	name := appstate.WAPatchRegularLow
	f.fetchEvents = func(context.Context, string, bool, bool) ([]any, error) {
		return nil, appstate.ErrMismatchingLTHash
	}
	fetchErr, persistErr := a.fetchAndPersistAppState(t.Context(), name, true, &appStatePersistenceTracker{})
	if persistErr != nil || !errors.Is(fetchErr, appstate.ErrMismatchingLTHash) {
		t.Fatalf("forced full sync = %v, %v; want ErrMismatchingLTHash", fetchErr, persistErr)
	}
	if !a.isAppStateFullSyncBroken(name) {
		t.Fatal("failing full sync did not flag the collection")
	}
	a.Close()

	reopened, err := New(Options{StoreDir: a.StoreDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if !reopened.isAppStateFullSyncBroken(name) {
		t.Fatal("the flag was not read back from wacli.db after a restart")
	}
}
