package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/store"
)

const (
	chatJID = "31600000001@s.whatsapp.net"
	testKey = "test-key-0123456789"
)

type received struct {
	Schema     int               `json:"schema"`
	StoreID    string            `json:"store_id"`
	Linked     bool              `json:"linked"`
	More       bool              `json:"more"`
	Messages   []Message         `json:"messages"`
	Removed    []Removed         `json:"removed"`
	Calls      []store.CallEvent `json:"calls"`
	keyID, sig string
	raw        []byte
}

// receiver is the other side: it records each request and answers status.
type receiver struct {
	mu     sync.Mutex
	got    []received
	status int
	srv    *httptest.Server
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	rc := &receiver{status: http.StatusOK}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		defer rc.mu.Unlock()
		if rc.status != http.StatusOK {
			w.WriteHeader(rc.status)
			return
		}
		req := received{keyID: r.Header.Get("X-Wacli-Key-Id"), sig: r.Header.Get("X-Wacli-Signature"), raw: body}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		rc.got = append(rc.got, req)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) take() []received {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	got := rc.got
	rc.got = nil
	return got
}

func (rc *receiver) answer(status int) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.status = status
}

type fixture struct {
	t    *testing.T
	db   *store.DB
	dir  string
	rc   *receiver
	opts Options
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "wacli.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.UpsertChat(chatJID, "dm", "Alice", time.Now()); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	rc := newReceiver(t)
	return &fixture{t: t, db: db, dir: dir, rc: rc, opts: Options{
		URL: rc.srv.URL, Key: []byte(testKey), AllowPrivate: true, StoreDir: dir, Version: "test", Linked: true,
	}}
}

func (f *fixture) message(id, text string) {
	f.t.Helper()
	if err := f.db.UpsertMessage(store.UpsertMessageParams{
		ChatJID: chatJID, MsgID: id, SenderJID: chatJID, SenderName: "Alice", Timestamp: time.Now(), Text: text,
	}); err != nil {
		f.t.Fatalf("UpsertMessage: %v", err)
	}
}

func (f *fixture) run() (Result, []received) {
	f.t.Helper()
	res, err := Run(context.Background(), f.db, f.opts)
	if err != nil {
		f.t.Fatalf("Run: %v", err)
	}
	return res, f.rc.take()
}

func messageIDs(reqs []received) []string {
	var ids []string
	for _, r := range reqs {
		for _, m := range r.Messages {
			ids = append(ids, m.MsgID)
		}
	}
	return ids
}

func TestFirstRunCopiesEverythingOnceAndSignsIt(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.message("b", "two")

	res, reqs := f.run()
	if got := messageIDs(reqs); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("first run sent %v, want [a b]", got)
	}
	if res.Messages != 2 || res.Requests != len(reqs) {
		t.Fatalf("result = %+v for %d requests", res, len(reqs))
	}
	for i, r := range reqs {
		if r.sig != signature([]byte(testKey), r.raw) || r.keyID != keyID([]byte(testKey)) {
			t.Fatalf("request %d: signature %q / key id %q do not match the key", i, r.sig, r.keyID)
		}
		if r.Schema != 1 || r.StoreID == "" || !r.Linked {
			t.Fatalf("request %d: envelope = schema %d, store %q, linked %v", i, r.Schema, r.StoreID, r.Linked)
		}
		if closing := i == len(reqs)-1; r.More == closing {
			t.Fatalf("request %d of %d has more=%v", i+1, len(reqs), r.More)
		}
	}

	// Nothing new: one closing request, the sign of life.
	res, reqs = f.run()
	if len(reqs) != 1 || reqs[0].More || res.Messages != 0 {
		t.Fatalf("second run = %+v, %d requests", res, len(reqs))
	}
}

func TestLaterRunsSendNewEditedAndRevokedMessages(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.message("b", "two")
	f.run()

	f.message("c", "three")
	if err := f.db.UpdateMessageText(chatJID, "a", "one, edited"); err != nil {
		t.Fatalf("UpdateMessageText: %v", err)
	}
	if err := f.db.MarkMessageRevoked(chatJID, "b"); err != nil {
		t.Fatalf("MarkMessageRevoked: %v", err)
	}

	_, reqs := f.run()
	texts := map[string]string{}
	var removed []Removed
	for _, r := range reqs {
		for _, m := range r.Messages {
			texts[m.MsgID] = m.Text
		}
		removed = append(removed, r.Removed...)
	}
	if len(texts) != 2 || texts["a"] != "one, edited" || texts["c"] != "three" {
		t.Fatalf("messages = %v, want a (edited) and c", texts)
	}
	if len(removed) != 1 || removed[0].MsgID != "b" || removed[0].Reason != store.MessageDeletionReasonWhatsAppRevoke {
		t.Fatalf("removed = %+v, want b as revoked", removed)
	}

	// What arrived is not sent again.
	if res, _ := f.run(); res.Messages != 0 || res.Removed != 0 {
		t.Fatalf("the run after it sent %+v again", res)
	}
}

func TestFailedRequestKeepsThePlace(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.run()

	f.message("b", "two")
	f.rc.answer(http.StatusInternalServerError)
	if _, err := Run(context.Background(), f.db, f.opts); err == nil || !strings.Contains(err.Error(), "answered 500") {
		t.Fatalf("Run against a failing receiver: err = %v", err)
	}
	f.rc.answer(http.StatusOK)
	if _, reqs := f.run(); !slices.Equal(messageIDs(reqs), []string{"b"}) {
		t.Fatalf("after the failure the run sent %v, want [b]", messageIDs(reqs))
	}
}

func TestRequestsAreSplitByCountAndSize(t *testing.T) {
	f := newFixture(t)
	f.message("a", "x")
	f.message("big", strings.Repeat("y", 2*maxItemBytes))
	f.message("c", "x")
	for i := range maxItems + 5 {
		if err := f.db.UpsertCallEvent(store.UpsertCallEventParams{
			ChatJID: chatJID, CallID: fmt.Sprintf("call%03d", i), EventType: "call_log", Timestamp: time.Now(),
		}); err != nil {
			t.Fatalf("UpsertCallEvent: %v", err)
		}
	}

	_, reqs := f.run()
	messages, calls := 0, 0
	for i, r := range reqs {
		items := len(r.Messages) + len(r.Calls)
		if items > maxItems {
			t.Fatalf("request %d holds %d items, more than %d", i, items, maxItems)
		}
		if items > 1 && len(r.raw) > maxItemBytes+1024 {
			t.Fatalf("request %d is %d bytes with %d items; an oversized message must go alone", i, len(r.raw), items)
		}
		messages += len(r.Messages)
		calls += len(r.Calls)
	}
	if messages != 3 || calls != maxItems+5 {
		t.Fatalf("sent %d messages and %d calls, want 3 and %d", messages, calls, maxItems+5)
	}
}

func TestExpiredJournalStartsOverFromTheFullCopy(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.run()
	f.message("b", "two")

	// The journal drops entries after its retention; do that by hand.
	raw, err := sql.Open("sqlite3", filepath.Join(f.dir, "wacli.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM message_changes`); err != nil {
		t.Fatalf("empty the journal: %v", err)
	}
	_ = raw.Close()

	res, reqs := f.run()
	if got := messageIDs(reqs); !res.Restarted || !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("restarted=%v, sent %v; want a restart that sends [a b]", res.Restarted, got)
	}
	if res, reqs = f.run(); res.Restarted || len(messageIDs(reqs)) != 0 {
		t.Fatalf("the run after the restart sent %v again", messageIDs(reqs))
	}
}

func TestCallsGoOnceUnlessTheyCanStillChange(t *testing.T) {
	f := newFixture(t)
	call := func(id string, at time.Time, outcome string) {
		t.Helper()
		if err := f.db.UpsertCallEvent(store.UpsertCallEventParams{
			ChatJID: chatJID, CallID: id, EventType: "call_log", Direction: "outbound", Outcome: outcome, Timestamp: at,
		}); err != nil {
			t.Fatalf("UpsertCallEvent: %v", err)
		}
	}
	old := time.Now().Add(-72 * time.Hour)
	recent := time.Now().Add(-time.Minute)
	call("old", old, "connected")
	call("recent", recent, "")

	_, reqs := f.run()
	last := reqs[len(reqs)-1]
	if len(last.Calls) != 2 || last.More {
		t.Fatalf("closing request holds %d calls (more=%v), want both", len(last.Calls), last.More)
	}

	call("recent", recent, "missed")
	_, reqs = f.run()
	if calls := reqs[len(reqs)-1].Calls; len(calls) != 1 || calls[0].CallID != "recent" || calls[0].Outcome != "missed" {
		t.Fatalf("second run calls = %+v, want only the recent one with its outcome", calls)
	}
}

func TestOnlyTheListedMessageFieldsLeave(t *testing.T) {
	f := newFixture(t)
	if err := f.db.UpsertMessage(store.UpsertMessageParams{
		ChatJID: chatJID, MsgID: "img", SenderJID: chatJID, Timestamp: time.Now(), MediaType: "image",
		MediaCaption: "caption", Filename: "a.jpg", MimeType: "image/jpeg",
		DirectPath: "/v/secret-direct-path", MediaKey: []byte("secret-media-key"),
	}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	_, reqs := f.run()
	if strings.Contains(string(reqs[0].raw), "secret-") {
		t.Fatal("the request holds a field that fetches the attachment")
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(reqs[0].raw, &body); err != nil || len(body.Messages) != 1 {
		t.Fatalf("messages = %v (%v)", body.Messages, err)
	}
	allowed := []string{
		"chat_jid", "chat_name", "msg_id", "timestamp", "from_me", "sender_jid", "sender_name", "text",
		"display_text", "quoted_msg_id", "forwarded", "reaction_to_id", "reaction_emoji",
		"media_type", "media_caption", "filename", "mime_type", "edited",
	}
	for field := range body.Messages[0] {
		if !slices.Contains(allowed, field) {
			t.Fatalf("message field %q is not on the list of fields that may leave", field)
		}
	}
}

func TestDryRunPostsNothingAndKeepsNoPlace(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.opts.DryRun = true
	res, reqs := f.run()
	if len(reqs) != 0 || res.Messages != 1 || res.Requests == 0 {
		t.Fatalf("dry run: %d requests arrived, result %+v", len(reqs), res)
	}
	if left, _ := filepath.Glob(filepath.Join(f.dir, "push-*")); len(left) != 0 {
		t.Fatalf("dry run left %v", left)
	}
	f.opts.DryRun = false
	if _, reqs = f.run(); !slices.Equal(messageIDs(reqs), []string{"a"}) {
		t.Fatalf("the run after a dry run sent %v, want [a]", messageIDs(reqs))
	}
}

func TestRedirectIsNotFollowedAndPlainHTTPNeedsAllowPrivate(t *testing.T) {
	f := newFixture(t)
	elsewhere := newReceiver(t)
	redirect := httptest.NewServer(http.RedirectHandler(elsewhere.srv.URL, http.StatusTemporaryRedirect))
	t.Cleanup(redirect.Close)

	f.opts.URL = redirect.URL
	if _, err := Run(context.Background(), f.db, f.opts); err == nil || !strings.Contains(err.Error(), "answered 307") {
		t.Fatalf("Run against a redirect: err = %v", err)
	}
	if got := elsewhere.take(); len(got) != 0 {
		t.Fatalf("%d requests followed the redirect", len(got))
	}

	f.opts.AllowPrivate = false
	if _, err := Run(context.Background(), f.db, f.opts); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http without --allow-private: err = %v", err)
	}
}

func TestEachDestinationKeepsItsOwnPlace(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.run()

	second := newReceiver(t)
	f.opts.URL = second.srv.URL
	if _, err := Run(context.Background(), f.db, f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := messageIDs(second.take()); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("a second destination got %v, want the full copy [a]", got)
	}
	files, _ := filepath.Glob(filepath.Join(f.dir, "push-*.json"))
	if len(files) != 2 {
		t.Fatalf("position files = %v, want one per destination", files)
	}
	if info, err := os.Stat(files[0]); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("position file mode = %v, want owner-only", info.Mode())
	}
}
