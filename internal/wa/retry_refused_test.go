package wa

import (
	"path/filepath"
	"testing"
)

// whatsmeow answers a retry receipt by sending the message again, without
// going through the app's WAClient guard. The read-only build must refuse
// every retry and must not read the retry buffer a sending build left behind.
func TestClientNeverAnswersRetryReceipts(t *testing.T) {
	c, err := New(Options{StorePath: newPairedSessionStoreAt(t, filepath.Join(t.TempDir(), "session.db"))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.client.UseRetryMessageStore {
		t.Fatal("UseRetryMessageStore = true: stored sent messages would be sent again on a retry receipt")
	}
	if c.client.PreRetryCallback == nil || c.client.PreRetryCallback(nil, "", 0, nil) {
		t.Fatal("PreRetryCallback does not refuse: a retry receipt would be answered with a send")
	}
	if msg := c.client.GetMessageForRetry(c.client.Store.GetJID(), c.client.Store.GetJID(), "id"); msg != nil {
		t.Fatalf("GetMessageForRetry = %v, want nil", msg)
	}
}
