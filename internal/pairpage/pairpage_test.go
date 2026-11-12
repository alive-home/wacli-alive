package pairpage

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsc.io/qr"
)

// A code the size of a real one: a reference and three keys, comma separated.
var sampleCode = "2@" + strings.Repeat("Qk9HVVNSRUY", 12) + "," + strings.Repeat("a1B2c3D4e5F", 4) + "," + strings.Repeat("Z9y8X7w6V5u", 4) + "," + strings.Repeat("m3N4o5P6q7R", 4)

func readPage(t *testing.T, dir string) (html, js string) {
	t.Helper()
	h, err := os.ReadFile(filepath.Join(dir, htmlName))
	if err != nil {
		t.Fatal(err)
	}
	j, err := os.ReadFile(filepath.Join(dir, jsName))
	if err != nil {
		t.Fatal(err)
	}
	return string(h), string(j)
}

func wantState(t *testing.T, dir, state string) (html, js string) {
	t.Helper()
	html, js = readPage(t, dir)
	if !strings.Contains(html, `<body data-state="`+state+`">`) {
		t.Fatalf("pair.html is not in state %q", state)
	}
	if !strings.Contains(js, `"state":"`+state+`"`) {
		t.Fatalf("pair.js is not in state %q: %s", state, js)
	}
	return html, js
}

func TestPageFollowsPairing(t *testing.T) {
	dir := t.TempDir()
	p := New(dir, "")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	_, js := wantState(t, dir, stateStarting)
	if !strings.Contains(js, `"until":`) {
		t.Fatalf("a waiting page must say until when it is current: %s", js)
	}

	if err := p.ShowQR(sampleCode); err != nil {
		t.Fatal(err)
	}
	html, js := wantState(t, dir, stateScan)
	path, _, err := qrPath(sampleCode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `d="`+path+`"`) || !strings.Contains(js, path) {
		t.Fatal("the code is not on the page")
	}

	if err := p.Linked("31612345678"); err != nil {
		t.Fatal(err)
	}
	html, js = wantState(t, dir, stateSyncing)
	if strings.Contains(html, path) || strings.Contains(js, path) {
		t.Fatal("the pairing code must leave the page once linked")
	}
	if !strings.Contains(html, "+31612345678") {
		t.Fatal("the linked number is not on the page")
	}

	if err := p.Finish(1284, nil); err != nil {
		t.Fatal(err)
	}
	html, js = wantState(t, dir, stateDone)
	if !strings.Contains(html, `<span class="count">1284</span>`) || !strings.Contains(js, `"messages":1284`) {
		t.Fatal("the message count is not on the page")
	}
	if strings.Contains(js, `"until"`) || strings.Contains(html, `http-equiv="refresh"`) {
		t.Fatal("a finished page must not expire or reload")
	}

	// Nothing after Finish changes the page.
	if err := p.ShowQR(sampleCode); err != nil {
		t.Fatal(err)
	}
	wantState(t, dir, stateDone)
}

func TestFinishSaysHowItEnded(t *testing.T) {
	dir := t.TempDir()
	p := New(dir, "")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(0, errors.New("QR code timed out")); err != nil {
		t.Fatal(err)
	}
	wantState(t, dir, stateStopped)

	dir = t.TempDir()
	p = New(dir, "")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	if err := p.Linked("31612345678"); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(12, errors.New("connection lost")); err != nil {
		t.Fatal(err)
	}
	html, _ := wantState(t, dir, stateHalted)
	if !strings.Contains(html, "+31612345678") {
		t.Fatal("a halted page still names the linked number")
	}
}

func TestLivePageExpires(t *testing.T) {
	dir := t.TempDir()
	p := New(dir, "")
	at := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return at }
	if err := p.set(func(v *view) { v.State = stateScan }); err != nil {
		t.Fatal(err)
	}
	_, js := readPage(t, dir)
	want := `"until":` + strconv.FormatInt(at.Add(staleAfter).UnixMilli(), 10)
	if !strings.Contains(js, want) {
		t.Fatalf("pair.js = %s, want %s", js, want)
	}
	if heartbeat*2 >= staleAfter {
		t.Fatal("one missed heartbeat must not expire the page")
	}
}

func TestPageFilesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no owner-only mode bits on Windows")
	}
	dir := t.TempDir()
	p := New(dir, "")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Finish(0, nil)
	for _, name := range []string{htmlName, jsName} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", name, info.Mode().Perm())
		}
	}
}

// The page is opened from disk and must stay there: the only address in it is
// the link to the source, and nothing is fetched.
func TestPageLoadsNothingFromTheNetwork(t *testing.T) {
	path, size, err := qrPath(sampleCode)
	if err != nil {
		t.Fatal(err)
	}
	html, err := renderHTML(view{State: stateScan, QR: path, QRSize: size}, "/Users/anna/.wacli", "")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, addr := range regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]{2,}[^\s"'<)]*`).FindAllString(page, -1) {
		if addr != SourceURL {
			t.Errorf("unexpected address in the page: %s", addr)
		}
	}
	for _, loads := range []string{"<link", "<img", "<iframe", "@import", "url(", "fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource"} {
		if strings.Contains(page, loads) {
			t.Errorf("the page must not contain %q", loads)
		}
	}
	if !strings.Contains(page, `content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline' 'self' file:"`) {
		t.Error("the page must carry the policy that blocks every network load")
	}
	if got := strings.Count(page, "<script"); got != 1 {
		t.Errorf("%d script tags, want the one inline script", got)
	}
}

func TestPageEscapesWhatItIsGiven(t *testing.T) {
	dir := `/tmp/</code><script>alert(1)</script>"&`
	html, err := renderHTML(view{State: stateSyncing, Phone: `"><script>alert(2)</script>`}, dir, `--store "`+dir+`"`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>alert(") {
		t.Fatal("markup from a path or number reached the page unescaped")
	}
}

func TestQRPathDrawsEveryDarkModule(t *testing.T) {
	path, size, err := qrPath(sampleCode)
	if err != nil {
		t.Fatal(err)
	}
	c, err := qr.Encode(sampleCode, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	if size != c.Size+2*quietZone {
		t.Fatalf("size = %d, want %d", size, c.Size+2*quietZone)
	}
	drawn := make(map[[2]int]bool)
	for _, m := range regexp.MustCompile(`M(\d+) (\d+)h(\d+)v1h-(\d+)z`).FindAllStringSubmatch(path, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		w, _ := strconv.Atoi(m[3])
		for i := 0; i < w; i++ {
			drawn[[2]int{x + i - quietZone, y - quietZone}] = true
		}
	}
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) != drawn[[2]int{x, y}] {
				t.Fatalf("module %d,%d: drawn %v, code says %v", x, y, drawn[[2]int{x, y}], c.Black(x, y))
			}
		}
	}
}

func TestPromptPointsAtTheStore(t *testing.T) {
	plain := Prompt("en", "/Users/anna/.wacli", "")
	if !strings.Contains(plain, "- wacli chats list: ") || !strings.Contains(plain, "/Users/anna/.wacli") {
		t.Fatalf("prompt = %s", plain)
	}
	elsewhere := Prompt("nl", "/data/wa", `--store "/data/wa"`)
	for _, line := range strings.Split(elsewhere, "\n") {
		if strings.HasPrefix(line, "- wacli ") && !strings.HasPrefix(line, `- wacli --store "/data/wa" `) {
			t.Fatalf("command does not reach the store: %s", line)
		}
	}
	if !strings.Contains(elsewhere, "`wacli --store \"/data/wa\" auth --open`") {
		t.Fatalf("prompt = %s", elsewhere)
	}
}

// Writes every state of the page for a look in a browser:
//
//	WACLI_PAIRPAGE_SAMPLES=/tmp/pairpage go test ./internal/pairpage -run TestWriteSamples
func TestWriteSamples(t *testing.T) {
	root := os.Getenv("WACLI_PAIRPAGE_SAMPLES")
	if root == "" {
		t.Skip("set WACLI_PAIRPAGE_SAMPLES to a directory to write the sample pages")
	}
	store := "/Users/anna/.wacli"
	if runtime.GOOS == "windows" {
		store = `C:\Users\Annemarie van der Berg\AppData\Local\wacli`
	}
	samples := map[string]view{
		stateStarting: {State: stateStarting},
		stateScan:     {State: stateScan},
		stateSyncing:  {State: stateSyncing, Phone: "31612345678"},
		stateDone:     {State: stateDone, Phone: "31612345678", Messages: 18432},
		stateHalted:   {State: stateHalted, Phone: "31612345678", Messages: 212},
		stateStopped:  {State: stateStopped},
	}
	for name, v := range samples {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := New(dir, "")
		// Far enough ahead that a sample is still current when it is looked at.
		p.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
		if v.State == stateScan {
			path, size, err := qrPath(sampleCode)
			if err != nil {
				t.Fatal(err)
			}
			v.QR, v.QRSize = path, size
		}
		if err := p.set(func(cur *view) { *cur = v }); err != nil {
			t.Fatal(err)
		}
		// The sample shows the customer's folder, not the temp one it sits in.
		html, err := renderHTML(p.cur, store, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Path(), html, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log(p.Path())
	}
}
