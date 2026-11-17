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
	"reflect"
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
	SentAt     time.Time         `json:"sent_at"`
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

func removedIDs(reqs []received) []string {
	var ids []string
	for _, r := range reqs {
		for _, m := range r.Removed {
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
		if age := time.Since(r.SentAt); age < -time.Minute || age > time.Minute {
			t.Fatalf("request %d: sent_at = %v, want the moment it left", i, r.SentAt)
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
	f.message("gone", "taken back")
	f.run()
	f.message("b", "two")
	if err := f.db.MarkMessageRevoked(chatJID, "gone"); err != nil {
		t.Fatalf("MarkMessageRevoked: %v", err)
	}

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
	// The journal no longer says so, but the receiver still holds its text.
	if got := removedIDs(reqs); !slices.Equal(got, []string{"gone"}) {
		t.Fatalf("the restart told the removal of %v, want [gone]", got)
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
	// The list in docs/push.md. A field added to Message is a field that leaves.
	listed := []string{
		"chat_jid", "chat_name", "msg_id", "timestamp", "from_me", "sender_jid", "sender_name", "text",
		"display_text", "quoted_msg_id", "forwarded", "reaction_to_id", "reaction_emoji",
		"media_type", "media_caption", "filename", "mime_type", "edited",
	}
	var fields []string
	for field := range reflect.TypeFor[Message]().Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		fields = append(fields, name)
	}
	if !slices.Equal(fields, listed) {
		t.Fatalf("message fields = %v, want exactly the listed %v", fields, listed)
	}
}

// A message that was taken back before the first run goes as a removal, and
// its text never leaves.
func TestFullCopyTellsRemovalsWithoutTheirText(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.message("b", "taken-back-text")
	if err := f.db.MarkMessageRevoked(chatJID, "b"); err != nil {
		t.Fatalf("MarkMessageRevoked: %v", err)
	}

	res, reqs := f.run()
	if !slices.Equal(messageIDs(reqs), []string{"a"}) || !slices.Equal(removedIDs(reqs), []string{"b"}) || res.Removed != 1 {
		t.Fatalf("sent %v, removed %v", messageIDs(reqs), removedIDs(reqs))
	}
	for _, r := range reqs {
		if strings.Contains(string(r.raw), "taken-back-text") {
			t.Fatal("the text of a revoked message left")
		}
	}
}

// WhatsApp swaps a hidden id for the phone number: the chat moves, and each
// message in it has to arrive under its new chat.
func TestAChatThatMovesKeepsItsMessages(t *testing.T) {
	const hidden, phone = "123456789@lid", "31611111111@s.whatsapp.net"
	f := newFixture(t)
	if err := f.db.UpsertChat(hidden, "dm", "Bob", time.Now()); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	if err := f.db.UpsertMessage(store.UpsertMessageParams{
		ChatJID: hidden, MsgID: "m1", SenderJID: hidden, Timestamp: time.Now(), Text: "hello",
	}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	f.run()

	if err := f.db.MigrateLIDToPN(hidden, phone); err != nil {
		t.Fatalf("MigrateLIDToPN: %v", err)
	}
	_, reqs := f.run()
	var moved []Removed
	var arrived []Message
	for _, r := range reqs {
		moved = append(moved, r.Removed...)
		arrived = append(arrived, r.Messages...)
	}
	if len(moved) != 1 || moved[0].ChatJID != hidden || moved[0].MovedToChatJID != phone || moved[0].MovedToMsgID != "m1" {
		t.Fatalf("removed = %+v, want m1 moved from the hidden chat to the phone chat", moved)
	}
	if len(arrived) != 1 || arrived[0].ChatJID != phone || arrived[0].MsgID != "m1" || arrived[0].Text != "hello" {
		t.Fatalf("messages = %+v, want m1 under the phone chat", arrived)
	}
}

// A failure says why, and never repeats the URL: its path can hold a token.
func TestFailedRequestNamesTheCauseNotTheURL(t *testing.T) {
	f := newFixture(t)
	f.rc.srv.Close()
	f.opts.URL = f.rc.srv.URL + "/secret-token"
	_, err := Run(context.Background(), f.db, f.opts)
	if err == nil || !strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err = %v, want the cause without the URL", err)
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

func TestRedirectIsNotFollowed(t *testing.T) {
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
}

func TestPlainHTTPOnlyReachesThisComputerWithoutAllowPrivate(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	f.opts.AllowPrivate = false

	// The fixture's receiver listens on 127.0.0.1.
	if _, err := Run(context.Background(), f.db, f.opts); err != nil {
		t.Fatalf("Run to a receiver on this computer: %v", err)
	}
	if got := messageIDs(f.rc.take()); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("the receiver on this computer got %v, want [a]", got)
	}

	for _, elsewhere := range []string{"http://example.com/in", "http://192.168.1.10/in", "http://localhost.example.com/in"} {
		f.opts.URL = elsewhere
		if _, err := Run(context.Background(), f.db, f.opts); err == nil || !strings.Contains(err.Error(), "https") {
			t.Fatalf("%s without --allow-private: err = %v", elsewhere, err)
		}
	}
}

func TestANameThatLeavesThisComputerIsNotDialled(t *testing.T) {
	for address, want := range map[string]bool{"127.0.0.1:80": true, "[::1]:80": true, "192.168.1.10:80": false, "93.184.216.34:80": false} {
		if err := onlyThisComputer("tcp", address, nil); (err == nil) != want {
			t.Errorf("onlyThisComputer(%s) = %v, want allowed %v", address, err, want)
		}
	}
}

func TestReceiverPromptNamesWhatIsPosted(t *testing.T) {
	prompt := ReceiverPrompt("Dutch")
	// Every field of a posted message, removal and call, by the name it is sent under.
	for _, v := range []any{Message{}, Removed{}, store.CallEvent{}} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var sent map[string]any
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatal(err)
		}
		for name := range sent {
			if !strings.Contains(prompt, name) {
				t.Errorf("the prompt does not name %q, which every %T carries", name, v)
			}
		}
	}
	for _, want := range []string{"X-Wacli-Key-Id", "X-Wacli-Signature", `"schema": 1`, "at most 200 items", "256 KB", "person + chat_jid + msg_id", "verify_jwt = false", "X-Wacli-Read-Key", "X-Wacli-Enroll-Secret", "WACLI_READ_KEY", "WACLI_ENROLL_SECRET", "A status request", `"status": true`, "last_read_at", "reads_today", "Read key:", "Answer me in Dutch."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt misses %q", want)
		}
	}
	if strings.Contains(prompt, "%!") {
		t.Errorf("the prompt has a formatting fault:\n%s", prompt)
	}
	if strings.Contains(ReceiverPrompt(""), "Answer me in") {
		t.Error("without a language the prompt still names one")
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

// The check before a schedule is installed: one signed request without
// content that says more follows, which leaves the full copy still to do.
func TestCheckProvesTheURLAndKeyWithoutSendingContent(t *testing.T) {
	f := newFixture(t)
	f.message("a", "one")
	if err := Check(context.Background(), f.db, f.opts); err != nil {
		t.Fatalf("Check: %v", err)
	}
	reqs := f.rc.take()
	if len(reqs) != 1 || !reqs[0].More || len(reqs[0].Messages) != 0 || !strings.HasPrefix(reqs[0].sig, "sha256=") {
		t.Fatalf("check sent %+v, want one signed request with more=true and no messages", reqs)
	}
	f.rc.answer(http.StatusUnauthorized)
	if err := Check(context.Background(), f.db, f.opts); err == nil || !strings.Contains(err.Error(), "answered 401") {
		t.Fatalf("Check against a receiver that refuses the key: err = %v", err)
	}
	f.rc.answer(http.StatusOK)
	f.rc.take()
	if _, reqs := f.run(); !slices.Equal(messageIDs(reqs), []string{"a"}) {
		t.Fatalf("after the check the first run sent %v, want [a]", messageIDs(reqs))
	}
}
