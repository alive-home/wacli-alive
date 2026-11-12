package wa

import (
	"context"
	"path/filepath"
	"testing"
)

func TestForgetDeviceClearsThePairingLocally(t *testing.T) {
	ctx := context.Background()
	path := newPairedSessionStoreAt(t, filepath.Join(t.TempDir(), "session.db"))
	authed := func() bool {
		c, err := New(Options{StorePath: path})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.IsAuthed()
	}
	if !authed() {
		t.Fatal("paired store is not authed before ForgetDevice")
	}
	if forgot, err := ForgetDevice(ctx, path); err != nil || !forgot {
		t.Fatalf("ForgetDevice = %v, %v, want true, nil", forgot, err)
	}
	if authed() {
		t.Fatal("store is still authed after ForgetDevice")
	}
	if forgot, err := ForgetDevice(ctx, path); err != nil || forgot {
		t.Fatalf("second ForgetDevice = %v, %v, want false, nil", forgot, err)
	}
}
