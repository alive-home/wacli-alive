package main

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/store"
)

// A real Unix endpoint and child CLI verify lock delegation, selector fidelity,
// caller-side verification and output publication without a WhatsApp account.
func TestMediaDownloadDelegatesExactRecoveryAndVerifiesOutput(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	for _, mode := range []string{"valid", "wrong-message", "wrong-file", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			dir := shortDelegateStoreDir(t)
			data := []byte("exact own-account synthetic recording")
			hash := sha256.Sum256(data)
			db, err := store.Open(filepath.Join(dir, "wacli.db"))
			if err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertChat("111@s.whatsapp.net", "dm", "Synthetic", time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertMessage(store.UpsertMessageParams{ChatJID: "111@s.whatsapp.net", MsgID: "exact", SenderJID: "111@s.whatsapp.net", Timestamp: time.Now(), MediaType: "audio", MimeType: "audio/ogg", DirectPath: "expired-location", MediaKey: make([]byte, 32), FileSHA256: hash[:], FileLength: uint64(len(data))}); err != nil {
				t.Fatal(err)
			}
			db.Close()
			lk, err := lock.Acquire(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer lk.Release()
			canonical := filepath.Join(dir, "recovered.ogg")
			if mode == "wrong-file" {
				data = []byte("a different recording")
			}
			if err := os.WriteFile(canonical, data, 0o600); err != nil {
				t.Fatal(err)
			}
			server := startDelegateTestSocket(t, dir, func(req delegateRequest) delegateResponse {
				if mode == "unsupported" {
					return delegateResponse{OK: false, Error: "unsupported kind media_download"}
				}
				id := "exact"
				if mode == "wrong-message" {
					id = "another"
				}
				return delegateResponse{OK: true, Media: &mediaDownloadResult{Chat: "111@s.whatsapp.net", ID: id, Path: canonical, Bytes: int64(len(data)), Source: "phone", Downloaded: true}}
			})
			defer server.stop()
			target := filepath.Join(dir, "output.ogg")
			if err := os.WriteFile(target, []byte("preserve existing output"), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runDelegateHelper(t, []string{"--store", dir, "--json", "--timeout", "1s", "media", "download", "--chat", "111@s.whatsapp.net", "--id", "exact", "--output", target, "--retry-on-phone"})
			req := server.nextRequest(t)
			if req.Kind != mediaDownloadKind || req.To != "111@s.whatsapp.net" || req.ID != "exact" || !req.RetryOnPhone {
				t.Fatalf("incorrect delegation: %+v", req)
			}
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "valid" {
				if err != nil || string(got) != string(data) || !strings.Contains(stdout, `"source":"phone"`) {
					t.Fatalf("valid recovery failed: %v stdout=%s stderr=%s output=%q", err, stdout, stderr, got)
				}
			} else if err == nil || string(got) != "preserve existing output" {
				t.Fatalf("bad response published output: %v stdout=%s stderr=%s output=%q", err, stdout, stderr, got)
			}
		})
	}
}

// A blocked phone request must neither delay the other requests nor allow
// another recovery request to dispatch after its deadline while queued.
func TestMediaDelegateWaitBypassesQueueAndSerializesRecovery(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	dir := shortDelegateStoreDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var mediaCalls atomic.Int32
	stop, err := startDelegateServerForStore(ctx, dir, func(ctx context.Context, req delegateRequest) (delegateResponse, error) {
		if req.Kind == mediaDownloadKind {
			mediaCalls.Add(1)
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return delegateResponse{}, ctx.Err()
			}
			return delegateResponse{OK: true, Media: &mediaDownloadResult{Source: "phone"}}, nil
		}
		return delegateResponse{OK: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	first := make(chan error, 1)
	go func() {
		_, err := askDelegate(ctx, &rootFlags{storeDir: dir, timeout: time.Second}, delegateRequest{Kind: mediaDownloadKind, To: "111@s.whatsapp.net", ID: "one"})
		first <- err
	}()
	<-entered
	defer close(release)
	if _, err := askDelegate(ctx, &rootFlags{storeDir: dir, timeout: 200 * time.Millisecond}, delegateRequest{Kind: contactsCheckKind, Phones: []string{"+15550000001"}}); err != nil {
		t.Fatalf("media blocked a contacts check: %v", err)
	}
	if _, err := askDelegate(ctx, &rootFlags{storeDir: dir, timeout: 60 * time.Millisecond}, delegateRequest{Kind: mediaDownloadKind, To: "111@s.whatsapp.net", ID: "two"}); err == nil {
		t.Fatal("queued media should expire")
	}
	if mediaCalls.Load() != 1 {
		t.Fatalf("queued recovery dispatched: %d", mediaCalls.Load())
	}
	select {
	case err := <-first:
		t.Fatalf("first recovery ended before release: %v", err)
	default:
	}
}

func TestMediaDelegateRetainsEarlierDeadlineAndCancelsAfterDial(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	for _, explicitCancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[explicitCancel], func(t *testing.T) {
			dir := shortDelegateStoreDir(t)
			entered := make(chan delegateRequest, 1)
			ended := make(chan struct{})
			stop, err := startDelegateServerForStore(context.Background(), dir, func(requestCtx context.Context, req delegateRequest) (delegateResponse, error) {
				entered <- req
				<-requestCtx.Done()
				close(ended)
				return delegateResponse{}, requestCtx.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			// Start the caller deadline after socket setup, and bound every wait.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			result := make(chan error, 1)
			start := time.Now()
			go func() {
				_, err := askDelegate(ctx, &rootFlags{storeDir: dir, timeout: 5 * time.Second}, delegateRequest{Kind: mediaDownloadKind, To: "111@s.whatsapp.net", ID: "exact"})
				result <- err
			}()
			var req delegateRequest
			select {
			case req = <-entered:
			case err := <-result:
				t.Fatalf("delegate ended before entering the handler: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("delegate did not enter the handler")
			}
			if explicitCancel {
				cancel()
			}
			select {
			case err = <-result:
			case <-time.After(2 * time.Second):
				t.Fatal("delegate ignored caller cancellation or deadline")
			}
			if err == nil || time.Since(start) > 2*time.Second {
				t.Fatalf("earlier context ignored: elapsed=%s err=%v", time.Since(start), err)
			}
			if req.DeadlineUnixMS > deadline.UnixMilli() {
				t.Fatalf("deadline renewed: request=%d caller=%d", req.DeadlineUnixMS, deadline.UnixMilli())
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("owner did not cancel closed media connection")
			}
		})
	}
}
