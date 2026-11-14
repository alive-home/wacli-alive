package app

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/openclaw/wacli/internal/sqliteutil"
	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waAdv"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// sdkSessionWA applies contact patches with the pinned SDK's own application
// step (ApplyAppStatePatches over a real session sqlstore with
// the journal installed, MAC validation on); only the server's response is
// supplied by the test.
type sdkSessionWA struct {
	*fakeWA
	device *waStore.Device
}

func newSDKSession(t *testing.T, f *fakeWA, keyID []byte, version uint64) (*waStore.Device, *whatsmeow.Client, *appstate.Processor) {
	t.Helper()
	ctx := t.Context()
	container, err := sqlstore.New(ctx, "sqlite3", sqliteutil.FileURI(filepath.Join(t.TempDir(), "session.db"), "_foreign_keys=on"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Close() })
	device := container.NewDevice()
	device.ID = &types.JID{User: "15550000000", Device: 7, Server: types.DefaultUserServer}
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := device.AppStateKeys.PutAppStateSyncKey(ctx, keyID, waStore.AppStateSyncKey{Data: bytes.Repeat([]byte{7}, 32), Fingerprint: []byte{1}, Timestamp: 1}); err != nil {
		t.Fatal(err)
	}
	if err := device.AppState.PutAppStateVersion(ctx, string(contactCollection), version, [128]byte{}); err != nil {
		t.Fatal(err)
	}
	f.journal = wa.NewContactStoreJournal(device.Contacts)
	device.Contacts = f.journal
	device.AppState = f.journal.AppStateStore(device.AppState)
	sdk := whatsmeow.NewClient(device, nil)
	return device, sdk, appstate.NewProcessor(device, waLog.Noop)
}
