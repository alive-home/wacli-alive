package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/delegate"
	"go.mau.fi/whatsmeow/types"
)

func TestPresenceMediaFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    types.ChatPresenceMedia
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "audio", input: "audio", want: types.ChatPresenceMediaAudio},
		{name: "trimmed case", input: " Audio ", want: types.ChatPresenceMediaAudio},
		{name: "unknown", input: "video", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := presenceMediaFromString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("presenceMediaFromString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPresenceStateFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    types.ChatPresence
		wantErr bool
	}{
		{name: "composing", input: "composing", want: types.ChatPresenceComposing},
		{name: "paused", input: "paused", want: types.ChatPresencePaused},
		{name: "trimmed case", input: " Paused ", want: types.ChatPresencePaused},
		{name: "unknown", input: "available", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := presenceStateFromString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("presenceStateFromString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

const (
	presenceDelegateHelperEnv     = "WACLI_PRESENCE_DELEGATE_HELPER"
	presenceDelegateHelperArgsEnv = "WACLI_PRESENCE_DELEGATE_ARGS"
)

func TestPresenceDelegateHelper(t *testing.T) {
	if os.Getenv(presenceDelegateHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(presenceDelegateHelperArgsEnv)), &args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode helper args: %v\n", err)
		os.Exit(2)
	}
	if err := execute(args); err != nil {
		os.Exit(1)
	}
}

func TestSendPresenceWithRetryReconnectsOnNotConnected(t *testing.T) {
	var sends int
	var reconnects int
	err := sendPresenceWithRetry(contextWithTestTimeout(t), func(context.Context) error {
		reconnects++
		return nil
	}, func(context.Context) error {
		sends++
		if sends == 1 {
			return errors.New("not connected")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sendPresenceWithRetry: %v", err)
	}
	if sends != 2 {
		t.Fatalf("send attempts = %d, want 2", sends)
	}
	if reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", reconnects)
	}
}

type fakePresenceWatchApp struct {
	jid   types.JID
	wait  time.Duration
	state *app.PresenceState
	err   error
}

func (f *fakePresenceWatchApp) WatchPresence(_ context.Context, jid types.JID, wait time.Duration) (*app.PresenceState, error) {
	f.jid, f.wait = jid, wait
	return f.state, f.err
}

func TestExecuteDelegatedPresenceSubscribe(t *testing.T) {
	online := &app.PresenceState{JID: "15551234567@s.whatsapp.net", Online: true}
	fake := &fakePresenceWatchApp{state: online}
	resp, err := executeDelegatedPresenceSubscribe(contextWithTestTimeout(t), fake, sendDelegateRequest{
		Kind: presenceSubscribeKind, To: "+1 (555) 123-4567", PresenceWaitMS: 2500,
	})
	if err != nil {
		t.Fatalf("executeDelegatedPresenceSubscribe: %v", err)
	}
	if fake.jid.String() != "15551234567@s.whatsapp.net" || fake.wait != 2500*time.Millisecond {
		t.Fatalf("watched %s for %s, want 15551234567@s.whatsapp.net for 2.5s", fake.jid, fake.wait)
	}
	if !resp.OK || resp.To != "15551234567@s.whatsapp.net" || resp.Presence != online {
		t.Fatalf("response = %+v", resp)
	}

	if _, err := executeDelegatedPresenceSubscribe(contextWithTestTimeout(t), fake, sendDelegateRequest{
		Kind: presenceSubscribeKind, To: "+15551234567", PresenceWaitMS: -1,
	}); err == nil {
		t.Fatalf("a negative wait was accepted")
	}
	quiet := &fakePresenceWatchApp{err: app.ErrPresenceQuiet}
	if _, err := executeDelegatedPresenceSubscribe(contextWithTestTimeout(t), quiet, sendDelegateRequest{
		Kind: presenceSubscribeKind, To: "+15551234567",
	}); !errors.Is(err, app.ErrPresenceQuiet) {
		t.Fatalf("error = %v, want the quiet sync's refusal", err)
	}
}

// A presence subscription can wait seconds for WhatsApp's answer. It must not
// queue behind a send that holds the delegate's send slot (a large upload), nor
// take that slot itself.
func TestPresenceSubscribeIsNotQueuedBehindSends(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}

	sendSlot := make(chan struct{}, 1) // empty: a send is in progress and owns it
	executed := make(chan sendDelegateRequest, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleSendDelegateConn(context.Background(), serverConn, func(_ context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
			executed <- req
			return sendDelegateResponse{OK: true, To: req.To}, nil
		}, sendSlot, newSendPacer(sendSpacing{}))
	}()

	if err := json.NewEncoder(clientConn).Encode(sendDelegateRequest{Version: sendDelegateVersion, Kind: presenceSubscribeKind, To: "x"}); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var resp sendDelegateResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("decode response (queued behind the send?): %v", err)
	}
	if !resp.OK || len(executed) != 1 {
		t.Fatalf("response = %+v, executed %d", resp, len(executed))
	}
	<-done
}

type presenceDelegateTestServer struct {
	requests chan presenceDelegateTestRequest
	stop     func()
}

type presenceDelegateTestRequest struct {
	req sendDelegateRequest
	err error
}

func startPresenceDelegateTestSocket(t *testing.T, storeDir string, respond func(sendDelegateRequest) sendDelegateResponse) *presenceDelegateTestServer {
	t.Helper()
	ln, err := delegate.Listen(storeDir)
	if err != nil {
		t.Fatalf("listen delegate socket: %v", err)
	}
	requests := make(chan presenceDelegateTestRequest, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

		var req sendDelegateRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			requests <- presenceDelegateTestRequest{err: err}
			return
		}
		requests <- presenceDelegateTestRequest{req: req}
		_ = json.NewEncoder(conn).Encode(respond(req))
	}()

	return &presenceDelegateTestServer{
		requests: requests,
		stop: func() {
			_ = ln.Close()
			<-done
		},
	}
}

func (s *presenceDelegateTestServer) nextRequest(t *testing.T) sendDelegateRequest {
	t.Helper()
	select {
	case got := <-s.requests:
		if got.err != nil {
			t.Fatalf("decode delegate request: %v", got.err)
		}
		return got.req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delegate request")
		return sendDelegateRequest{}
	}
}

func runPresenceDelegateHelper(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal helper args: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPresenceDelegateHelper$")
	cmd.Env = append(os.Environ(),
		presenceDelegateHelperEnv+"=1",
		presenceDelegateHelperArgsEnv+"="+string(rawArgs),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func shortPresenceDelegateStoreDir(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if runtime.GOOS == "darwin" {
		// Darwin has a short sockaddr_un path limit; the default TMPDIR under
		// /var/folders is often too long for .send.sock integration tests.
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "wacli-presence-*")
	if err != nil {
		t.Fatalf("temp store dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func skipPresenceDelegateSocketTestOnUnsupportedOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix send delegate socket integration is not available on Windows")
	}
}

func contextWithTestTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}
