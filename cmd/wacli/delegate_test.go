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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/delegate"
	"github.com/openclaw/wacli/internal/lock"
)

const (
	delegateHelperEnv     = "WACLI_DELEGATE_HELPER"
	delegateHelperArgsEnv = "WACLI_DELEGATE_ARGS"
)

// TestDelegateHelper is the wacli process of runDelegateHelper.
func TestDelegateHelper(t *testing.T) {
	if os.Getenv(delegateHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(delegateHelperArgsEnv)), &args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode helper args: %v\n", err)
		os.Exit(2)
	}
	if err := execute(args); err != nil {
		os.Exit(1)
	}
}

// runDelegateHelper runs wacli with args in a process of its own, as the
// second wacli that finds the store locked by a running sync.
func runDelegateHelper(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal helper args: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDelegateHelper$")
	cmd.Env = append(os.Environ(),
		delegateHelperEnv+"=1",
		delegateHelperArgsEnv+"="+string(rawArgs),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func shortDelegateStoreDir(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if runtime.GOOS == "darwin" {
		// Darwin has a short sockaddr_un path limit; the default TMPDIR under
		// /var/folders is often too long for the socket in the store directory.
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "wacli-delegate-*")
	if err != nil {
		t.Fatalf("temp store dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func skipDelegateSocketTestOnUnsupportedOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix delegate socket integration is not available on Windows")
	}
}

type delegateTestServer struct {
	requests <-chan delegateTestRequest
	stop     func()
}

type delegateTestRequest struct {
	req delegateRequest
	err error
}

// startDelegateTestSocket answers one request on the store's socket with
// respond and keeps the request for nextRequest.
func startDelegateTestSocket(t *testing.T, storeDir string, respond func(delegateRequest) delegateResponse) *delegateTestServer {
	t.Helper()
	ln, err := delegate.Listen(storeDir)
	if err != nil {
		t.Fatalf("listen delegate socket: %v", err)
	}
	requests := make(chan delegateTestRequest, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

		var req delegateRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			requests <- delegateTestRequest{err: err}
			return
		}
		requests <- delegateTestRequest{req: req}
		_ = json.NewEncoder(conn).Encode(respond(req))
	}()

	return &delegateTestServer{
		requests: requests,
		stop: func() {
			_ = ln.Close()
			<-done
		},
	}
}

func (s *delegateTestServer) nextRequest(t *testing.T) delegateRequest {
	t.Helper()
	select {
	case got := <-s.requests:
		if got.err != nil {
			t.Fatalf("decode delegate request: %v", got.err)
		}
		return got.req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delegate request")
		return delegateRequest{}
	}
}

func TestTryDelegateFallsBackWhenSocketUnavailable(t *testing.T) {
	flags := &rootFlags{storeDir: t.TempDir()}
	lockErr := fmt.Errorf("held: %w", lock.ErrLocked)

	_, delegated, err := tryDelegate(context.Background(), flags, lockErr, delegateRequest{Kind: contactsCheckKind})
	if delegated {
		t.Fatalf("delegated = true, want false for missing socket")
	}
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("error = %v, want original lock error", err)
	}
}

func TestTryDelegateDoesNotDelegateNonLockErrors(t *testing.T) {
	orig := errors.New("open store")

	_, delegated, err := tryDelegate(context.Background(), &rootFlags{}, orig, delegateRequest{Kind: contactsCheckKind})
	if delegated {
		t.Fatalf("delegated = true, want false")
	}
	if !errors.Is(err, orig) {
		t.Fatalf("error = %v, want original", err)
	}
}

func TestExecuteDelegatedRejectsBadVersionBeforeAppUse(t *testing.T) {
	_, err := executeDelegated(context.Background(), nil, delegateRequest{
		Version: delegateVersion + 1,
		Kind:    contactsCheckKind,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported delegate version") {
		t.Fatalf("error = %v", err)
	}
}

// A request still queued behind an earlier one when its caller gives up must
// be refused, never run late.

func unexpectedExecute(t *testing.T) delegateExecutor {
	return func(context.Context, delegateRequest) (delegateResponse, error) {
		t.Error("request ran after its caller's deadline")
		return delegateResponse{OK: true}, nil
	}
}

func freeSlot() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	return slot
}

// serveDelegateConn handles one request on an in-memory connection and
// returns the caller's end.
func serveDelegateConn(t *testing.T, ctx context.Context, execute delegateExecutor, slot chan struct{}) (client net.Conn, done <-chan struct{}) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		handleDelegateConn(ctx, serverConn, execute, slot, freeSlot())
	}()
	return clientConn, finished
}

func roundTripDelegate(t *testing.T, conn net.Conn, req delegateRequest) delegateResponse {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var resp delegateResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestDelegateQueueWaitHonorsRequestTimeout(t *testing.T) {
	slot := make(chan struct{}, 1) // empty: an earlier request owns it
	conn, done := serveDelegateConn(t, context.Background(), unexpectedExecute(t), slot)

	resp := roundTripDelegate(t, conn, delegateRequest{Version: delegateVersion, Kind: contactsCheckKind, TimeoutMS: 20})
	<-done
	if resp.OK || !strings.Contains(resp.Error, "it did not run") {
		t.Fatalf("response = %+v, want an explicit refusal", resp)
	}
}

func TestDelegateRefusesExpiredCallerDeadlineWithFreeSlot(t *testing.T) {
	slot := freeSlot()
	conn, done := serveDelegateConn(t, context.Background(), unexpectedExecute(t), slot)

	resp := roundTripDelegate(t, conn, delegateRequest{
		Version:        delegateVersion,
		Kind:           contactsCheckKind,
		TimeoutMS:      durationMillis(10 * time.Minute),
		DeadlineUnixMS: time.Now().Add(-time.Second).UnixMilli(),
	})
	<-done
	if resp.OK || !strings.Contains(resp.Error, "it did not run") {
		t.Fatalf("response = %+v, want an expired caller deadline to block dispatch", resp)
	}
	if len(slot) != 1 {
		t.Fatal("refused request did not return the slot")
	}
}

func TestDelegateReplyMarginLeavesRoomForRefusal(t *testing.T) {
	slot := make(chan struct{}, 1) // held by an earlier request
	conn, done := serveDelegateConn(t, context.Background(), unexpectedExecute(t), slot)

	// The caller stops reading at its deadline. The refusal must arrive first.
	callerDeadline := time.Now().Add(delegateReplyMargin + 200*time.Millisecond)
	if err := conn.SetDeadline(callerDeadline); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	req := delegateRequest{
		Version:        delegateVersion,
		Kind:           contactsCheckKind,
		TimeoutMS:      durationMillis(10 * time.Minute),
		DeadlineUnixMS: callerDeadline.UnixMilli(),
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var resp delegateResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("refusal did not arrive before the caller's deadline: %v", err)
	}
	<-done
	if resp.OK || !strings.Contains(resp.Error, "it did not run") {
		t.Fatalf("response = %+v, want an explicit refusal", resp)
	}
}

func TestDelegateShortTimeoutCanDispatch(t *testing.T) {
	conn, done := serveDelegateConn(t, context.Background(), func(ctx context.Context, _ delegateRequest) (delegateResponse, error) {
		return delegateResponse{OK: true}, ctx.Err()
	}, freeSlot())

	resp := roundTripDelegate(t, conn, delegateRequest{
		Version: delegateVersion, Kind: contactsCheckKind, TimeoutMS: 200,
		DeadlineUnixMS: time.Now().Add(200 * time.Millisecond).UnixMilli(),
	})
	<-done
	if !resp.OK {
		t.Fatalf("short request refused: %+v", resp)
	}
}

// Models a deadline whose cancellation callback has not run yet.
type delayedDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c delayedDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }

func TestDelegateRefusesElapsedDeadlineBeforeDispatch(t *testing.T) {
	parent := delayedDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
	conn, done := serveDelegateConn(t, parent, unexpectedExecute(t), freeSlot())

	resp := roundTripDelegate(t, conn, delegateRequest{Version: delegateVersion, Kind: contactsCheckKind, TimeoutMS: 10000})
	<-done
	if resp.OK || !strings.Contains(resp.Error, "it did not run") {
		t.Fatalf("expired request response: %+v", resp)
	}
}

// End to end: a request queued behind a slow one times out on the caller's
// side and must not run once the slow one finishes.
func TestDelegateServerDropsQueuedRequestAfterCallerTimeout(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortDelegateStoreDir(t)

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var executed atomic.Int32
	stop, err := startDelegateServerForStore(context.Background(), storeDir, func(ctx context.Context, req delegateRequest) (delegateResponse, error) {
		executed.Add(1)
		if req.ID == "slow" {
			started <- struct{}{}
			<-release
		}
		return delegateResponse{OK: true}, nil
	})
	if err != nil {
		t.Fatalf("start delegate server: %v", err)
	}
	defer stop()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	slowDone := make(chan error, 1)
	go func() {
		_, err := askDelegate(context.Background(), &rootFlags{storeDir: storeDir, timeout: 10 * time.Second}, delegateRequest{Kind: contactsCheckKind, ID: "slow"})
		slowDone <- err
	}()
	<-started

	queuedFlags := &rootFlags{storeDir: storeDir, timeout: delegateReplyMargin + 300*time.Millisecond}
	_, err = askDelegate(context.Background(), queuedFlags, delegateRequest{Kind: contactsCheckKind, ID: "queued"})
	if err == nil || !strings.Contains(err.Error(), "it did not run") {
		t.Fatalf("queued request error = %v, want an explicit refusal", err)
	}

	close(release)
	if err := <-slowDone; err != nil {
		t.Fatalf("slow request: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // give a late dispatch the chance to happen
	if got := executed.Load(); got != 1 {
		t.Fatalf("executed %d requests, want only the slow one", got)
	}
}

func TestAskDelegateReportsNoReplyBeforeTimeout(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortDelegateStoreDir(t)
	ln, err := delegate.Listen(storeDir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req delegateRequest
		_ = json.NewDecoder(conn).Decode(&req)
		time.Sleep(2 * time.Second) // accepted, never answers in time
	}()

	flags := &rootFlags{storeDir: storeDir, timeout: 200 * time.Millisecond}
	_, err = askDelegate(context.Background(), flags, delegateRequest{Kind: contactsCheckKind})
	if err == nil || !strings.Contains(err.Error(), "no reply from the running sync process") {
		t.Fatalf("error = %v, want it to say the running sync did not reply", err)
	}
}
