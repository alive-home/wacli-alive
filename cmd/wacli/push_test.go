package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/schedule"
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
		"--url is required":                   {"push"},
		"at least 1m":                         {"push", "--url", "https://example.test/in", "--every", "10s"},
		"cannot be combined":                  {"push", "--url", "https://example.test/in", "--dry-run", "--sync"},
		"read-only mode":                      {"--read-only", "push", "--url", "https://example.test/in", "--sync"},
		"the URL must use https":              {"push", "--url", "http://example.test/in"},
		"cannot be combined with other flags": {"push", "--installed", "--url", "https://example.test/in"},
	} {
		err := execute(append([]string{"--store", t.TempDir()}, args...))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%v: err = %v, want %q", args, err, want)
		}
	}
}

// fakeScheduler stands in for launchd and Task Scheduler.
type fakeScheduler struct {
	jobs    map[string]schedule.Job
	removed []string
	refuse  error
}

func useFakeScheduler(t *testing.T) *fakeScheduler {
	t.Helper()
	fake := &fakeScheduler{jobs: map[string]schedule.Job{}}
	real := pushScheduler
	pushScheduler.install = func(job schedule.Job) error {
		if fake.refuse != nil {
			return fake.refuse
		}
		fake.jobs[job.Label] = job
		return nil
	}
	pushScheduler.uninstall = func(label string) error {
		fake.removed = append(fake.removed, label)
		delete(fake.jobs, label)
		return nil
	}
	pushScheduler.installed = func(label string) (bool, error) { _, ok := fake.jobs[label]; return ok, nil }
	t.Cleanup(func() { pushScheduler = real })
	return fake
}

// only returns the one job the fake holds.
func (f *fakeScheduler) only(t *testing.T) schedule.Job {
	t.Helper()
	if len(f.jobs) != 1 {
		t.Fatalf("scheduled %d jobs, want 1", len(f.jobs))
	}
	for _, job := range f.jobs {
		return job
	}
	return schedule.Job{}
}

type installReceiver struct {
	status int
	more   []bool
	srv    *httptest.Server
}

func newInstallReceiver(t *testing.T) *installReceiver {
	t.Helper()
	rc := &installReceiver{status: http.StatusNoContent}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env struct {
			More bool `json:"more"`
		}
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Errorf("request body: %v", err)
		}
		rc.more = append(rc.more, env.More)
		w.WriteHeader(rc.status)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

// install proves the receiver first, keeps the URL out of the job, and the
// job's round leaves how it ended for `push status`.
func TestPushInstallChecksSavesAndSchedules(t *testing.T) {
	fake := useFakeScheduler(t)
	rc := newInstallReceiver(t)
	store := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := execute([]string{"--store", store, "push", "install", "--url", rc.srv.URL + "/in/secret-token", "--key-file", keyFile, "--allow-private"}); err != nil {
		t.Fatalf("push install: %v", err)
	}
	if len(rc.more) != 1 || !rc.more[0] {
		t.Fatalf("install sent more=%v, want one request that says more follows", rc.more)
	}
	job := fake.only(t)
	args := strings.Join(job.Args, " ")
	if !strings.HasPrefix(job.Label, "wacli-push-") || job.Every != 15*time.Minute || !strings.HasSuffix(args, " push --installed") || !strings.Contains(args, "--store ") {
		t.Fatalf("job = %+v", job)
	}
	if strings.Contains(args, "secret-token") || strings.Contains(args, keyFile) {
		t.Fatalf("the job's arguments hold the URL or the key file: %s", args)
	}
	info, err := os.Stat(filepath.Join(store, "scheduled-push.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("settings file mode = %v, want owner-only", info.Mode().Perm())
	}

	// The scheduled round on a store that was never paired: the sync fails,
	// what is stored still goes, and the failure is what status reports.
	err = execute([]string{"--store", store, "push", "--installed"})
	if err == nil || !strings.Contains(err.Error(), "the sync before it failed") {
		t.Fatalf("push --installed: err = %v", err)
	}
	if len(rc.more) != 2 || rc.more[1] {
		t.Fatalf("the round sent more=%v, want a closing request after the check", rc.more)
	}
	last, err := os.ReadFile(filepath.Join(store, "scheduled-push-last.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(last), `"ok":false`) || strings.Contains(string(last), "secret-token") {
		t.Fatalf("last run = %s", last)
	}

	if err := execute([]string{"--store", store, "push", "uninstall"}); err != nil {
		t.Fatalf("push uninstall: %v", err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != job.Label {
		t.Fatalf("removed = %v", fake.removed)
	}
	for _, name := range []string{"scheduled-push.json", "scheduled-push-last.json"} {
		if _, err := os.Stat(filepath.Join(store, name)); !os.IsNotExist(err) {
			t.Fatalf("%s is still there after uninstall: %v", name, err)
		}
	}
	if err := execute([]string{"--store", store, "push", "--installed"}); err == nil || !strings.Contains(err.Error(), "no scheduled push is installed") {
		t.Fatalf("push --installed after uninstall: err = %v", err)
	}
}

// A program hands the URL and the key over standard input. The key is kept
// in the store, owner-only, signs the check and the scheduled rounds, and
// goes when the schedule is removed.
func TestPushInstallTakesTheURLAndKeyFromStandardInput(t *testing.T) {
	fake := useFakeScheduler(t)
	var wrong []string
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("a-key"))
		mac.Write(body)
		requests++
		if got, want := r.Header.Get("X-Wacli-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
			wrong = append(wrong, got)
		}
	}))
	defer srv.Close()
	store := t.TempDir()
	input, err := json.Marshal(map[string]string{"url": srv.URL + "/in/secret-token", "key": "a-key"})
	if err != nil {
		t.Fatal(err)
	}

	root := newRootCmd(&rootFlags{})
	root.SetIn(strings.NewReader(string(input)))
	root.SetArgs([]string{"--store", store, "push", "install", "--stdin", "--allow-private"})
	if err := root.Execute(); err != nil {
		t.Fatalf("push install --stdin: %v", err)
	}
	fake.only(t)
	key := filepath.Join(store, "scheduled-push.key")
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("key file mode = %v, want owner-only", info.Mode().Perm())
	}
	// The round's sync fails on a store that was never paired; what it posts is still signed.
	_ = execute([]string{"--store", store, "push", "--installed"})
	if requests != 2 || len(wrong) != 0 {
		t.Fatalf("%d requests, %d not signed with the key from standard input: %q", requests, len(wrong), wrong)
	}
	if err := execute([]string{"--store", store, "push", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatalf("the key is still there after uninstall: %v", err)
	}

	root = newRootCmd(&rootFlags{})
	root.SetIn(strings.NewReader("a-key\n"))
	root.SetArgs([]string{"--store", store, "push", "install", "--stdin", "--allow-private"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), `want {"url"`) {
		t.Fatalf("push install --stdin with a bare key: err = %v", err)
	}
}

// A scheduler that does not take the job leaves nothing that would push
// later: no saved URL, no stored key, no job from an earlier install.
func TestPushInstallLeavesNothingWhenTheSchedulerRefuses(t *testing.T) {
	fake := useFakeScheduler(t)
	rc := newInstallReceiver(t)
	store := t.TempDir()
	input, err := json.Marshal(map[string]string{"url": rc.srv.URL, "key": "a-key"})
	if err != nil {
		t.Fatal(err)
	}
	install := func() error {
		root := newRootCmd(&rootFlags{})
		root.SetIn(strings.NewReader(string(input)))
		root.SetArgs([]string{"--store", store, "push", "install", "--stdin", "--allow-private"})
		return root.Execute()
	}
	if err := install(); err != nil {
		t.Fatalf("first install: %v", err)
	}

	fake.refuse = errors.New("launchctl bootstrap: 5: Input/output error")
	err = install()
	if err == nil || !strings.Contains(err.Error(), "nothing is installed") || !strings.Contains(err.Error(), "Input/output error") {
		t.Fatalf("second install: err = %v", err)
	}
	if len(fake.jobs) != 0 {
		t.Fatalf("jobs left: %v", fake.jobs)
	}
	for _, name := range []string{"scheduled-push.json", "scheduled-push.key", "scheduled-push-last.json"} {
		if _, err := os.Stat(filepath.Join(store, name)); !os.IsNotExist(err) {
			t.Fatalf("%s is left behind: %v", name, err)
		}
	}
}

// Each store has its own job: installing for a second store leaves the first
// one's schedule alone, and removing one leaves the other.
func TestPushInstallKeepsOneJobPerStore(t *testing.T) {
	fake := useFakeScheduler(t)
	rc := newInstallReceiver(t)
	first, second := t.TempDir(), t.TempDir()
	for _, store := range []string{first, second} {
		if err := execute([]string{"--store", store, "push", "install", "--url", rc.srv.URL, "--allow-private"}); err != nil {
			t.Fatalf("push install: %v", err)
		}
	}
	if len(fake.jobs) != 2 {
		t.Fatalf("jobs = %v, want one per store", fake.jobs)
	}
	if err := execute([]string{"--store", first, "push", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	job := fake.only(t)
	if args := strings.Join(job.Args, " "); !strings.Contains(args, filepath.Base(second)) {
		t.Fatalf("the job that is left runs %s, want the second store's", args)
	}
	if _, err := os.Stat(filepath.Join(second, "scheduled-push.json")); err != nil {
		t.Fatalf("the second store lost its settings: %v", err)
	}
}

// A receiver that refuses the key is found at install, not a day later.
func TestPushInstallInstallsNothingWhenTheReceiverRefuses(t *testing.T) {
	fake := useFakeScheduler(t)
	rc := newInstallReceiver(t)
	rc.status = http.StatusUnauthorized
	store := t.TempDir()

	err := execute([]string{"--store", store, "push", "install", "--url", rc.srv.URL, "--allow-private"})
	if err == nil || !strings.Contains(err.Error(), "nothing was installed") || !strings.Contains(err.Error(), "answered 401") {
		t.Fatalf("push install: err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(store, "scheduled-push.json")); len(fake.jobs) != 0 || !os.IsNotExist(statErr) {
		t.Fatalf("jobs = %v, settings file: %v", fake.jobs, statErr)
	}
}

func TestPushPromptPrintsWithoutAStore(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-made")
	text := captureRootStdout(t, func() {
		if err := execute([]string{"--store", missing, "push", "prompt", "--lang", "nl"}); err != nil {
			t.Fatalf("push prompt: %v", err)
		}
	})
	if !strings.Contains(text, "X-Wacli-Signature") || !strings.Contains(text, "Answer me in Dutch.") {
		t.Fatalf("push prompt printed:\n%s", text)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("push prompt made the store directory: %v", err)
	}
}

// `status --receiver` asks the receiver of the installed push, signed with the
// installed key, and a receiver without a status leaves the rest of status true.
func TestPushStatusAsksTheReceiverOnlyWhenTold(t *testing.T) {
	useFakeScheduler(t)
	answer, asked := `{"last_read_at":"2026-11-17T10:31:00Z","reads_today":3,"messages":1204,"calls":17}`, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Status bool `json:"status"`
		}
		_ = json.Unmarshal(body, &env)
		if !env.Status {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		asked++
		mac := hmac.New(sha256.New, []byte("a-key"))
		mac.Write(body)
		if r.Header.Get("X-Wacli-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Errorf("the status request is not signed with the installed key")
		}
		if answer == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	store := t.TempDir()
	root := newRootCmd(&rootFlags{})
	root.SetIn(strings.NewReader(`{"url":"` + srv.URL + `/in/secret-token","key":"a-key"}`))
	root.SetArgs([]string{"--store", store, "push", "install", "--stdin", "--allow-private"})
	captureRootStdout(t, func() {
		if err := root.Execute(); err != nil {
			t.Fatalf("push install: %v", err)
		}
	})
	status := func(args ...string) string {
		return captureRootStdout(t, func() {
			if err := execute(append([]string{"--store", store, "--json", "push", "status"}, args...)); err != nil {
				t.Fatalf("push status %v: %v", args, err)
			}
		})
	}

	if text := status(); asked != 0 || strings.Contains(text, "receiver") {
		t.Fatalf("a plain status asked the receiver %d times and printed:\n%s", asked, text)
	}
	text := status("--receiver")
	if asked != 1 || !strings.Contains(text, `"last_read_at":"2026-11-17T10:31:00Z"`) || !strings.Contains(text, `"reads_today":3`) || !strings.Contains(text, `"installed":true`) {
		t.Fatalf("status --receiver asked %d times and printed:\n%s", asked, text)
	}
	answer = ""
	text = status("--receiver")
	if !strings.Contains(text, `"receiver_error"`) || strings.Contains(text, `"receiver":`) || !strings.Contains(text, `"installed":true`) || strings.Contains(text, "secret-token") {
		t.Fatalf("status --receiver against a receiver without a status printed:\n%s", text)
	}

	if err := execute([]string{"--store", store, "push", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	asked = 0
	if text := status("--receiver"); asked != 0 || strings.Contains(text, "receiver") {
		t.Fatalf("with nothing installed status --receiver asked %d times and printed:\n%s", asked, text)
	}
}

// The scheduled round waits for a wacli that holds the store, which is the
// app still fetching the first history when the push was just turned on. It
// used to report "store is locked" as a failed round for the next 15 minutes.
func TestScheduledRoundWaitsForTheStore(t *testing.T) {
	useFakeScheduler(t)
	rc := newInstallReceiver(t)
	store := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := execute([]string{"--store", store, "push", "install", "--url", rc.srv.URL, "--key-file", keyFile, "--allow-private"}); err != nil {
		t.Fatalf("push install: %v", err)
	}

	held, err := lock.Acquire(store)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = held.Release()
		close(released)
	}()
	// The store was never paired, so the sync still fails: but on that, not on the lock.
	err = execute([]string{"--store", store, "push", "--installed"})
	<-released
	if err == nil || strings.Contains(err.Error(), "locked") {
		t.Fatalf("a round started while the store was held: err = %v, want the unpaired store's own failure", err)
	}

	// A wait that runs out is still reported.
	held, err = lock.Acquire(store)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	wait := scheduledLockWait
	scheduledLockWait = 50 * time.Millisecond
	defer func() { scheduledLockWait = wait }()
	if err := execute([]string{"--store", store, "push", "--installed"}); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("a round against a store that stays held: err = %v", err)
	}
}
