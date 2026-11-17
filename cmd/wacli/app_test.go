package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/pairpage"
)

// testApp is the page with a recorded wacli and a pairing round the test
// steers, served the way `wacli app` serves it.
type testApp struct {
	*appPage
	url string

	mu     sync.Mutex
	calls  []string // "push install --stdin < input"
	answer string
	fail   error
	rounds chan func(*pairpage.Page) error
}

func newTestApp(t *testing.T, lang string) *testApp {
	t.Helper()
	page, err := newAppPage(&rootFlags{}, t.TempDir(), lang, "")
	if err != nil {
		t.Fatal(err)
	}
	app := &testApp{appPage: page, answer: `{"installed":false,"scheduled":false,"host":"","last_run":null}`, rounds: make(chan func(*pairpage.Page) error, 4)}
	page.wacli = func(_ context.Context, input string, args ...string) (json.RawMessage, error) {
		app.mu.Lock()
		defer app.mu.Unlock()
		call := strings.Join(args, " ")
		if input != "" {
			call += " < " + input
		}
		app.calls = append(app.calls, call)
		return json.RawMessage(app.answer), app.fail
	}
	page.link = func(p *pairpage.Page) error { return (<-app.rounds)(p) }
	srv := httptest.NewServer(page)
	t.Cleanup(srv.Close)
	// Every round the test started has ended before the store directory goes.
	t.Cleanup(page.linkingDone.Wait)
	page.host = strings.TrimPrefix(srv.URL, "http://")
	app.url = page.address()
	return app
}

func (app *testApp) took() []string {
	app.mu.Lock()
	defer app.mu.Unlock()
	calls := app.calls
	app.calls = nil
	return calls
}

func (app *testApp) do(t *testing.T, method, path, body string, header map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, app.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range header {
		if name == "Host" {
			req.Host = value
			continue
		}
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(text)
}

func (app *testApp) fromPage() map[string]string {
	return map[string]string{"Origin": "http://" + app.host, "Content-Type": "application/json"}
}

// waitFor polls the page's own status until it says what the test waits for.
func (app *testApp) waitFor(t *testing.T, want string) string {
	t.Helper()
	var text string
	for range 200 {
		if _, text = app.do(t, http.MethodGet, "status", "", nil); strings.Contains(text, want) {
			app.took()
			return text
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status never said %s; last: %s", want, text)
	return ""
}

// The page is for the browser tab wacli opened and for nothing else on the
// computer or the network: not without the token, not under another name, and
// no change that did not come from the page itself.
func TestAppPageAnswersOnlyItsOwnPage(t *testing.T) {
	app := newTestApp(t, "")
	app.rounds <- func(*pairpage.Page) error { return nil }
	app.startLinking()
	app.waitFor(t, `"running":false`)

	for name, tc := range map[string]struct {
		method, url string
		header      map[string]string
		want        int
	}{
		"no token":             {http.MethodGet, "http://" + app.host + "/", nil, http.StatusNotFound},
		"a wrong token":        {http.MethodGet, "http://" + app.host + "/" + strings.Repeat("0", 32) + "/", nil, http.StatusNotFound},
		"another name":         {http.MethodGet, app.url, map[string]string{"Host": "evil.example"}, http.StatusNotFound},
		"a change, no origin":  {http.MethodPost, app.url + "off", map[string]string{"Content-Type": "application/json"}, http.StatusForbidden},
		"a change, elsewhere":  {http.MethodPost, app.url + "on", map[string]string{"Origin": "https://evil.example", "Content-Type": "application/json"}, http.StatusForbidden},
		"a change from a form": {http.MethodPost, app.url + "on", map[string]string{"Origin": "http://" + app.host, "Content-Type": "text/plain"}, http.StatusForbidden},
		"a change by GET":      {http.MethodGet, app.url + "off", nil, http.StatusNotFound},
		"a file of the store":  {http.MethodGet, app.url + "wacli.db", nil, http.StatusNotFound},
	} {
		req, err := http.NewRequest(tc.method, tc.url, strings.NewReader(`{"url":"https://evil.example/","key":"k"}`))
		if err != nil {
			t.Fatal(err)
		}
		for header, value := range tc.header {
			if header == "Host" {
				req.Host = value
				continue
			}
			req.Header.Set(header, value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", name, resp.StatusCode, tc.want)
		}
	}
	if calls := app.took(); len(calls) != 0 {
		t.Fatalf("a refused request still ran wacli: %q", calls)
	}
}

// The page follows the pairing as the file page does, speaks the browser's
// language, and carries the panel.
func TestAppPageShowsPairingAndThePanel(t *testing.T) {
	app := newTestApp(t, "")
	scanned := make(chan struct{})
	app.rounds <- func(page *pairpage.Page) error {
		if err := page.ShowQR("2@a-code"); err != nil {
			return err
		}
		<-scanned
		if err := page.Linked("31612345678"); err != nil {
			return err
		}
		return page.Finish(42, nil)
	}
	app.startLinking()
	app.waitFor(t, `"running":true`)

	code, html := app.do(t, http.MethodGet, "", "", map[string]string{"Accept-Language": "nl-NL,nl;q=0.9,en;q=0.8"})
	if code != http.StatusOK || !strings.Contains(html, `<html lang="nl">`) || !strings.Contains(html, `id="turn-on"`) || !strings.Contains(html, "Kopieer opdracht") {
		t.Fatalf("the page: status %d, Dutch panel missing", code)
	}
	if _, html := app.do(t, http.MethodGet, "", "", map[string]string{"Accept-Language": "de-DE"}); !strings.Contains(html, `<html lang="en">`) {
		t.Fatal("a browser in another language must get English")
	}
	for range 200 {
		if _, script := app.do(t, http.MethodGet, "pair.js", "", nil); strings.Contains(script, `"state":"scan"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, script := app.do(t, http.MethodGet, "pair.js", "", nil); !strings.HasPrefix(script, "pairState(") || !strings.Contains(script, `"state":"scan"`) {
		t.Fatalf("pair.js while a code is up: %s", script)
	}
	close(scanned)
	app.waitFor(t, `"running":false`)
	if _, script := app.do(t, http.MethodGet, "pair.js", "", nil); !strings.Contains(script, `"state":"done"`) || !strings.Contains(script, `"messages":42`) {
		t.Fatalf("pair.js after the fetch: %s", script)
	}

	_, prompt := app.do(t, http.MethodGet, "prompt", "", map[string]string{"Accept-Language": "nl"})
	if !strings.Contains(prompt, "X-Wacli-Signature") || !strings.Contains(prompt, "Answer me in Dutch.") {
		t.Fatalf("the prompt: %.200s", prompt)
	}
}

// The panel's buttons are `push install`, `uninstall` and `status`, with the
// address and the key over standard input and never in the arguments.
func TestAppPageRunsThePushCommands(t *testing.T) {
	app := newTestApp(t, "en")
	app.rounds <- func(*pairpage.Page) error { return nil }
	app.startLinking()
	app.waitFor(t, `"running":false`)

	input := `{"url":"https://example.com/in","key":"a-key"}`
	app.answer = `{"installed":true,"scheduled":true,"host":"example.com","last_run":null}`
	code, text := app.do(t, http.MethodPost, "on", input, app.fromPage())
	if calls := app.took(); code != http.StatusOK || len(calls) != 1 || calls[0] != "push install --stdin < "+input {
		t.Fatalf("on: status %d, wacli ran %q", code, calls)
	}
	if !strings.Contains(text, `"host":"example.com"`) || !strings.Contains(text, `"asked":false`) {
		t.Fatalf("on answered %s", text)
	}

	_, text = app.do(t, http.MethodGet, "status?receiver=1", "", nil)
	if calls := app.took(); len(calls) != 1 || calls[0] != "push status --receiver" || !strings.Contains(text, `"asked":true`) {
		t.Fatalf("status with the receiver: wacli ran %q, answered %s", calls, text)
	}
	app.do(t, http.MethodGet, "status", "", nil)
	if calls := app.took(); len(calls) != 1 || calls[0] != "push status" {
		t.Fatalf("status: wacli ran %q", calls)
	}

	app.fail = errors.New("nothing was installed: push: example.com answered 401")
	_, text = app.do(t, http.MethodPost, "on", input, app.fromPage())
	if !strings.Contains(text, `"error":"nothing was installed: push: example.com answered 401"`) || strings.Contains(text, `"push"`) {
		t.Fatalf("a refused install answered %s", text)
	}
	app.fail = nil
	app.took()

	app.do(t, http.MethodPost, "off", "{}", app.fromPage())
	if calls := app.took(); len(calls) != 1 || calls[0] != "push uninstall" {
		t.Fatalf("off: wacli ran %q", calls)
	}
}

// A round that ends early says why, and the page can start another.
func TestAppPageStartsLinkingAgain(t *testing.T) {
	app := newTestApp(t, "")
	app.rounds <- func(*pairpage.Page) error { return errors.New("the code was not scanned in time") }
	app.startLinking()
	text := app.waitFor(t, `"running":false`)
	if !strings.Contains(text, `"failure":"the code was not scanned in time"`) {
		t.Fatalf("status after a failed round: %s", text)
	}
	if _, script := app.do(t, http.MethodGet, "pair.js", "", nil); !strings.Contains(script, `"state":"stopped"`) {
		t.Fatalf("pair.js after a failed round: %s", script)
	}

	app.rounds <- func(page *pairpage.Page) error {
		if err := page.Linked("31612345678"); err != nil {
			return err
		}
		return page.Finish(7, nil)
	}
	if code, _ := app.do(t, http.MethodPost, "link", "{}", app.fromPage()); code != http.StatusOK {
		t.Fatalf("link: status %d", code)
	}
	text = app.waitFor(t, `"failure":""`)
	app.waitFor(t, `"running":false`)
	if _, script := app.do(t, http.MethodGet, "pair.js", "", nil); !strings.Contains(script, `"state":"done"`) {
		t.Fatalf("pair.js after the second round: %s (status %s)", script, text)
	}
}

// wacli leaves when the page has been closed for a while, never while it is
// pairing or while a tab still asks.
func TestAppPageLeavesWhenNobodyLooks(t *testing.T) {
	app := newTestApp(t, "")
	release := make(chan struct{})
	app.rounds <- func(*pairpage.Page) error { <-release; return nil }
	app.startLinking()
	for range appIdleTicks + 2 {
		if app.idle() {
			t.Fatal("idle while pairing")
		}
	}
	close(release)
	app.waitFor(t, `"running":false`)
	for i := range appIdleTicks + 2 {
		app.do(t, http.MethodGet, "pair.js", "", nil)
		if app.idle() {
			t.Fatalf("idle at check %d while a tab still asks", i)
		}
	}
	for i := 1; i < appIdleTicks; i++ {
		if app.idle() {
			t.Fatalf("idle after %d quiet checks, want %d", i, appIdleTicks)
		}
	}
	if !app.idle() {
		t.Fatalf("not idle after %d quiet checks", appIdleTicks)
	}
}

// Serves the page in each state of the panel for a look in a browser:
//
//	WACLI_APP_SAMPLES=127.0.0.1:8792 go test ./cmd/wacli -run TestAppPageSamples -timeout 0
func TestAppPageSamples(t *testing.T) {
	addr := os.Getenv("WACLI_APP_SAMPLES")
	if addr == "" {
		t.Skip("set WACLI_APP_SAMPLES to an address to serve the sample pages")
	}
	ago := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339) }
	linked := func(page *pairpage.Page) error {
		if err := page.Linked("31612345678"); err != nil {
			return err
		}
		return page.Finish(19570, nil)
	}
	samples := []struct {
		name, status string
		round        func(*pairpage.Page) error
	}{
		{"off", `{"installed":false,"scheduled":false,"host":"","last_run":null}`, linked},
		{"on", `{"installed":true,"scheduled":true,"host":"abcdefghijklmnopqrst.supabase.co","last_run":{"at":"` + ago(4*time.Minute) + `","ok":true,"result":{"requests":1,"messages":12,"removed":0,"calls":1}},"receiver":{"last_read_at":"` + ago(3*time.Minute) + `","reads_today":7,"messages":19570,"calls":323}}`, linked},
		{"first-run", `{"installed":true,"scheduled":true,"host":"crm.example.com","last_run":null,"receiver":{"last_read_at":null,"reads_today":0,"messages":0,"calls":0}}`, linked},
		{"failed", `{"installed":true,"scheduled":true,"host":"abcdefghijklmnopqrst.supabase.co","last_run":{"at":"` + ago(26*time.Hour) + `","ok":false,"error":"push: abcdefghijklmnopqrst.supabase.co answered 401"},"receiver_error":"push: abcdefghijklmnopqrst.supabase.co answered 401"}`, linked},
		{"not-scheduled", `{"installed":true,"scheduled":false,"host":"crm.example.com","last_run":{"at":"` + ago(3*time.Hour) + `","ok":true,"result":{"requests":1,"messages":1,"removed":0,"calls":0}}}`, linked},
		{"stopped", `{"installed":false,"scheduled":false,"host":"","last_run":null}`, func(*pairpage.Page) error {
			return errors.New("store is locked (another wacli is running?)")
		}},
	}
	mux := http.NewServeMux()
	for _, sample := range samples {
		page, err := newAppPage(&rootFlags{}, t.TempDir(), "", addr)
		if err != nil {
			t.Fatal(err)
		}
		page.token = sample.name
		page.wacli = func(context.Context, string, ...string) (json.RawMessage, error) {
			return json.RawMessage(sample.status), nil
		}
		page.link = sample.round
		page.startLinking()
		mux.Handle("/"+sample.name+"/", page)
		fmt.Printf("http://%s/%s/\n", addr, sample.name)
	}
	t.Fatal(http.ListenAndServe(addr, mux))
}
