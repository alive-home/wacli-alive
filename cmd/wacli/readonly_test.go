package main

import (
	"os"
	"strings"
	"testing"
)

// The read-only build must not expose send, poll or presence commands.
func TestReadOnlyBuildDoesNotRegisterMutatingCommands(t *testing.T) {
	for _, name := range []string{"send", "poll", "presence"} {
		err := execute([]string{"--store", t.TempDir(), name})
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("%s: err = %v, want unknown command", name, err)
		}
	}
}

// Follow mode must not open the send-delegate socket: a delegated text fetches
// its link preview (an outbound request to an arbitrary host) before the
// WAClient guard gets to reject the send.
func TestSyncDoesNotStartSendDelegateServer(t *testing.T) {
	src, err := os.ReadFile("sync.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "startSendDelegateServer") {
		t.Fatal("sync.go starts the send-delegate server; the read-only build must not")
	}
}
