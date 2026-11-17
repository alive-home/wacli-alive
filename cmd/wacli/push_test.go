package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An empty store still posts its one closing request: signed with the key
// from the file, and saying the store has no WhatsApp session.
func TestPushCommandPostsASignedSignOfLife(t *testing.T) {
	var bodies [][]byte
	var signatures []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		signatures = append(signatures, r.Header.Get("X-Wacli-Signature"))
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := execute([]string{"--store", t.TempDir(), "push", "--url", srv.URL, "--key-file", keyFile, "--allow-private"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(bodies) != 1 || !strings.HasPrefix(signatures[0], "sha256=") {
		t.Fatalf("got %d requests, signatures %q", len(bodies), signatures)
	}
	var env struct {
		Linked   bool              `json:"linked"`
		More     bool              `json:"more"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(bodies[0], &env); err != nil {
		t.Fatal(err)
	}
	if env.Linked || env.More || env.Messages == nil || len(env.Messages) != 0 {
		t.Fatalf("envelope = %+v, want linked=false, more=false, messages=[]", env)
	}
}

func TestPushCommandRefusesBadFlagCombinations(t *testing.T) {
	for want, args := range map[string][]string{
		"--url is required":      {"push"},
		"at least 1m":            {"push", "--url", "https://example.test/in", "--every", "10s"},
		"cannot be combined":     {"push", "--url", "https://example.test/in", "--dry-run", "--sync"},
		"read-only mode":         {"--read-only", "push", "--url", "https://example.test/in", "--sync"},
		"the URL must use https": {"push", "--url", "http://example.test/in"},
	} {
		err := execute(append([]string{"--store", t.TempDir()}, args...))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%v: err = %v, want %q", args, err, want)
		}
	}
}
