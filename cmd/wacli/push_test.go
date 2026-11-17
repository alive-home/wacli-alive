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
	"time"

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
	jobs    []schedule.Job
	removed []string
}

func useFakeScheduler(t *testing.T) *fakeScheduler {
	t.Helper()
	fake := &fakeScheduler{}
	real := pushScheduler
	pushScheduler.install = func(job schedule.Job) error { fake.jobs = append(fake.jobs, job); return nil }
	pushScheduler.uninstall = func(label string) error { fake.removed = append(fake.removed, label); return nil }
	pushScheduler.installed = func(string) (bool, error) { return len(fake.jobs) > len(fake.removed), nil }
	t.Cleanup(func() { pushScheduler = real })
	return fake
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
	if len(fake.jobs) != 1 {
		t.Fatalf("scheduled %d jobs, want 1", len(fake.jobs))
	}
	job := fake.jobs[0]
	args := strings.Join(job.Args, " ")
	if job.Label != "wacli-push" || job.Every != 15*time.Minute || !strings.HasSuffix(args, " push --installed") || !strings.Contains(args, "--store ") {
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
	if len(fake.removed) != 1 || fake.removed[0] != "wacli-push" {
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

// A key handed over standard input is kept in the store, owner-only, signs the
// scheduled rounds, and goes when the schedule is removed.
func TestPushInstallKeepsAKeyFromStandardInput(t *testing.T) {
	useFakeScheduler(t)
	var signatures []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signatures = append(signatures, r.Header.Get("X-Wacli-Signature"))
	}))
	defer srv.Close()
	store := t.TempDir()

	root := newRootCmd(&rootFlags{})
	root.SetIn(strings.NewReader("a-key\n"))
	root.SetArgs([]string{"--store", store, "push", "install", "--url", srv.URL, "--key-stdin", "--allow-private"})
	if err := root.Execute(); err != nil {
		t.Fatalf("push install --key-stdin: %v", err)
	}
	key := filepath.Join(store, "scheduled-push.key")
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("key file mode = %v, want owner-only", info.Mode().Perm())
	}
	_ = execute([]string{"--store", store, "push", "--installed"})
	if len(signatures) != 2 || !strings.HasPrefix(signatures[0], "sha256=") || !strings.HasPrefix(signatures[1], "sha256=") {
		t.Fatalf("signatures = %q, want the check and the round both signed", signatures)
	}
	if err := execute([]string{"--store", store, "push", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatalf("the key is still there after uninstall: %v", err)
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
