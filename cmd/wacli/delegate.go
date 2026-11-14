package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/delegate"
	"github.com/openclaw/wacli/internal/lock"
)

// A running `sync --follow` holds the store lock and the only connection to
// WhatsApp. Another wacli process that needs that connection for a read asks
// the running one over a socket in the store directory, with one JSON request
// and one JSON reply. readOnlyDelegateKinds lists what can be asked.

const (
	delegateVersion       = 1
	delegateResponseGrace = 5 * time.Second
	// delegateReplyMargin is reserved before the caller's deadline so a refusal
	// can reach the caller before it gives up on the connection.
	delegateReplyMargin = 500 * time.Millisecond
)

var errDelegateUnavailable = errors.New("no running sync process to ask")

type delegateRequest struct {
	Version        int      `json:"version"`
	Kind           string   `json:"kind"`
	To             string   `json:"to,omitempty"`
	ID             string   `json:"id,omitempty"`
	Phones         []string `json:"phones,omitempty"`
	TimeoutMS      int64    `json:"timeout_ms,omitempty"`
	DeadlineUnixMS int64    `json:"deadline_unix_ms,omitempty"`
	RetryOnPhone   bool     `json:"retry_on_phone,omitempty"`
}

type delegateResponse struct {
	OK       bool                 `json:"ok"`
	Error    string               `json:"error,omitempty"`
	Contacts []contactCheckResult `json:"contacts,omitempty"`
	Media    *mediaDownloadResult `json:"media,omitempty"`
}

type delegateExecutor func(context.Context, delegateRequest) (delegateResponse, error)

func askDelegate(ctx context.Context, flags *rootFlags, req delegateRequest) (delegateResponse, error) {
	req.Version = delegateVersion
	req.TimeoutMS = durationMillis(flags.timeout)
	storeDir, err := resolveStoreDir(flags)
	if err != nil {
		return delegateResponse{}, err
	}
	conn, err := delegate.Dial(ctx, storeDir)
	if err != nil {
		return delegateResponse{}, fmt.Errorf("%w: %v", errDelegateUnavailable, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(commandTimeout(flags))
	if req.Kind == mediaDownloadKind {
		// Cache inspection and lock waits already consumed this command's
		// budget. Never grant recovery a new full timeout after those stages.
		if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
			deadline = parentDeadline
		}
		// DialContext alone does not cancel an established socket. Closing it
		// wakes a blocked read and tells the owner to cancel this media request.
		stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stopCancel()
	}
	req.DeadlineUnixMS = deadline.UnixMilli()
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return delegateResponse{}, err
	}
	var resp delegateResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		if req.Kind == mediaDownloadKind && ctx.Err() != nil {
			return delegateResponse{}, fmt.Errorf("media caller stopped waiting; recovery may have completed, check the exact local file before retrying: %w", ctx.Err())
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			if req.Kind == mediaDownloadKind {
				return delegateResponse{}, fmt.Errorf("no recovery reply before the timeout; check the exact verified local file before retrying: %w", err)
			}
			return delegateResponse{}, fmt.Errorf("no reply from the running sync process before the timeout: %w", err)
		}
		return delegateResponse{}, err
	}
	if !resp.OK {
		return delegateResponse{}, errors.New(resp.Error)
	}
	return resp, nil
}

// tryDelegate asks the running sync process when lockErr says the store is
// locked. delegated is false when there was nobody to ask, and the error is
// then lockErr.
func tryDelegate(ctx context.Context, flags *rootFlags, lockErr error, req delegateRequest) (resp delegateResponse, delegated bool, err error) {
	if !lock.IsLocked(lockErr) {
		return delegateResponse{}, false, lockErr
	}
	resp, err = askDelegate(ctx, flags, req)
	if err != nil {
		if errors.Is(err, errDelegateUnavailable) {
			return delegateResponse{}, false, lockErr
		}
		return delegateResponse{}, true, err
	}
	return resp, true, nil
}

func startDelegateServer(ctx context.Context, a *app.App) (func(), error) {
	return startDelegateServerForStore(ctx, a.StoreDir(), func(ctx context.Context, req delegateRequest) (delegateResponse, error) {
		return executeDelegated(ctx, a, req)
	})
}

func startDelegateServerForStore(ctx context.Context, storeDir string, execute delegateExecutor) (func(), error) {
	ln, err := delegate.Listen(storeDir)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	// One slot serializes requests. Waiting for it is bounded by each caller's
	// deadline, so a request still queued when its caller gives up is refused
	// instead of running late.
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	// Media waits for the phone and never holds up the other requests. Its own
	// slot makes concurrent exact downloads recheck the cache before asking the
	// phone again.
	mediaSlot := make(chan struct{}, 1)
	mediaSlot <- struct{}{}
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleDelegateConn(ctx, conn, execute, slot, mediaSlot)
		}
	}()

	return func() {
		_ = ln.Close()
		<-done
	}, nil
}

func handleDelegateConn(ctx context.Context, conn net.Conn, execute delegateExecutor, slot, mediaSlot chan struct{}) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	reply := func(resp delegateResponse, err error) {
		if err != nil {
			resp = delegateResponse{OK: false, Error: err.Error()}
		}
		_ = json.NewEncoder(conn).Encode(resp)
	}

	var req delegateRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		reply(delegateResponse{}, err)
		return
	}

	// Every request gets one budget: its own timeout, capped by the caller's
	// absolute deadline less the reply margin. Queueing and the request itself
	// share it.
	deadline := time.Now().Add(millisDuration(req.TimeoutMS, 5*time.Minute))
	if req.DeadlineUnixMS > 0 {
		callerDeadline := time.UnixMilli(req.DeadlineUnixMS)
		if callerDeadline.Before(deadline) {
			deadline = callerDeadline
		}
	}
	// Reserve at most a tenth of the remaining budget for the reply, so
	// sub-second requests still have time to run.
	if remaining := time.Until(deadline); remaining > 0 {
		deadline = deadline.Add(-min(delegateReplyMargin, remaining/10))
	}
	requestCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	deadline, _ = requestCtx.Deadline()
	// The fixed initial deadline only protects request decoding. A queued
	// request may run longer than five minutes, so keep the transport alive
	// through its budget and the final response write.
	_ = conn.SetDeadline(deadline.Add(delegateResponseGrace))

	expired := errors.New("request timed out in the queue of the running sync process; it did not run")
	if req.Kind == mediaDownloadKind {
		slot = mediaSlot
		expired = errors.New("media request expired before recovery dispatch")
		// Requests carry one JSON value and clients leave the socket open for
		// the reply. A peer disconnect cancels queued or running recovery; it
		// must not silently continue after a canceled caller goes away.
		// Drain trailing framing bytes, including a split JSON newline; only
		// EOF/read failure denotes disconnection, not that harmless whitespace.
		go func() { _, _ = io.Copy(io.Discard, conn); cancel() }()
	}

	select {
	case <-requestCtx.Done():
		reply(delegateResponse{}, expired)
		return
	case <-slot:
		defer func() { slot <- struct{}{} }()
	}
	// select picks at random when the slot frees up at the same moment the
	// deadline passes, and timer delivery can lag wall-clock expiry. Never
	// start a request after its caller gave up.
	if requestCtx.Err() != nil || !time.Now().Before(deadline) {
		reply(delegateResponse{}, expired)
		return
	}
	reply(execute(requestCtx, req))
}

func executeDelegated(parent context.Context, a *app.App, req delegateRequest) (delegateResponse, error) {
	if req.Version != delegateVersion {
		return delegateResponse{}, fmt.Errorf("unsupported delegate version %d", req.Version)
	}
	if !readOnlyDelegateKinds[req.Kind] {
		return delegateResponse{}, fmt.Errorf("%w: a running sync does not answer %q", errReadOnlyBuild, req.Kind)
	}
	ctx, cancel := context.WithTimeout(parent, millisDuration(req.TimeoutMS, 5*time.Minute))
	defer cancel()

	switch req.Kind {
	case mediaDownloadKind:
		if req.To == "" || req.ID == "" {
			return delegateResponse{}, fmt.Errorf("exact media chat and message ID are required")
		}
		result, err := executeExactMediaDownload(ctx, a, req.To, req.ID, req.RetryOnPhone)
		return delegateResponse{OK: err == nil, Media: result}, err
	case contactsCheckKind:
		return executeDelegatedContactsCheck(ctx, a.WA(), req)
	default:
		return delegateResponse{}, fmt.Errorf("%w: a running sync does not answer %q", errReadOnlyBuild, req.Kind)
	}
}

func durationMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(d / time.Millisecond)
}

func millisDuration(ms int64, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func commandTimeout(flags *rootFlags) time.Duration {
	if flags == nil || flags.timeout <= 0 {
		return 5 * time.Minute
	}
	return flags.timeout
}
