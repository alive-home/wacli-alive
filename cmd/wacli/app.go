package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openclaw/wacli/internal/pairpage"
	"github.com/openclaw/wacli/internal/push"
	"github.com/spf13/cobra"
)

// The app page leaves when nobody has looked at it for this long. An open
// tab asks every few seconds, and at least once a minute when it is hidden.
const (
	appIdleCheck = 30 * time.Second
	appIdleTicks = 6
)

type appOptions struct {
	lang   string
	noOpen bool
	port   int
}

// newAppCmd is wacli as a page in the browser, for a computer without the Mac
// app: the pairing page, with a panel that turns the scheduled push on and off
// and shows how it goes. Like the Mac app it holds no logic of its own: the
// page is the one `auth --page` writes, and the panel runs `push install`,
// `uninstall` and `status`.
func newAppCmd(flags *rootFlags) *cobra.Command {
	var opts appOptions
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Open wacli as a page in your browser: link WhatsApp, turn the push on, see how it goes",
		Long: "app links WhatsApp and fetches what is new, as `auth` does, and shows it on a page in your\n" +
			"browser. The page also turns the scheduled push on and off (`push install`, `push uninstall`)\n" +
			"and shows its last run and when the receiver last read. The page comes from this computer:\n" +
			"an address on 127.0.0.1 that only this page knows, gone when the page has been closed for a\n" +
			"few minutes. On Windows, opening wacli.exe from Explorer runs this command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runApp(flags, opts) },
	}
	cmd.Flags().StringVar(&opts.lang, "lang", "", "language of the page: nl or en (default: the browser's)")
	cmd.Flags().BoolVar(&opts.noOpen, "no-open", false, "print the address without opening a browser")
	cmd.Flags().IntVar(&opts.port, "port", 0, "port on 127.0.0.1 (default: a free one)")
	return cmd
}

func runApp(flags *rootFlags, opts appOptions) error {
	if err := flags.requireWritable(); err != nil {
		return err
	}
	storeDir, err := resolveStoreDir(flags)
	if err != nil {
		return err
	}
	// A scheduled push can hold the store for the length of one sync.
	if flags.lockWait == 0 {
		flags.lockWait = 2 * time.Minute
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.port))
	if err != nil {
		return fmt.Errorf("open the page's address: %w", err)
	}
	a, err := newAppPage(flags, storeDir, opts.lang, listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return err
	}
	server := &http.Server{Handler: a, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()

	address := a.address()
	fmt.Fprintf(os.Stderr, "wacli is open in your browser:\n  %s\nKeep this window open while you use the page. Closing it stops wacli; a push that is turned on keeps running.\n", address)
	if !opts.noOpen {
		if err := pairpage.Open(address); err != nil {
			fmt.Fprintf(os.Stderr, "Could not open a browser (%v). Open the address above yourself.\n", err)
		}
	}
	a.startLinking()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(appIdleCheck)
	defer ticker.Stop()
	for waiting := true; waiting; {
		select {
		case <-ctx.Done():
			waiting = false
		case <-ticker.C:
			waiting = !a.idle()
		}
	}
	shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shut)
	// The signal that ends this command also ends the sync; the store closes with it.
	a.linkingDone.Wait()
	return nil
}

// appPage serves the page and what its panel asks for.
type appPage struct {
	flags    *rootFlags
	storeDir string
	lang     string // "" takes each request's own language
	host     string // 127.0.0.1:port
	token    string
	// wacli runs this program with --json for this store and returns its data.
	wacli func(ctx context.Context, input string, args ...string) (json.RawMessage, error)
	// link is one round of pairing and fetching on page.
	link func(page *pairpage.Page) error

	mu          sync.Mutex
	page        *pairpage.Page
	linking     bool
	failure     string
	linkingDone sync.WaitGroup

	requests atomic.Int64
	seen     int64
	quiet    int
}

func newAppPage(flags *rootFlags, storeDir, lang, host string) (*appPage, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	a := &appPage{flags: flags, storeDir: storeDir, lang: lang, host: host, token: hex.EncodeToString(token)}
	a.wacli = a.runWacli
	a.link = func(page *pairpage.Page) error {
		_, err := runAuth(flags, authOptions{idleExit: 30 * time.Second, qrFormat: "terminal", page: page})
		return err
	}
	return a, nil
}

func (a *appPage) address() string { return "http://" + a.host + "/" + a.token + "/" }

// idle reports whether the page has gone unasked for long enough to leave. It
// counts checks, not minutes, so a computer that slept does not look idle the
// moment it wakes.
func (a *appPage) idle() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.requests.Load()
	if a.linking || now != a.seen {
		a.seen, a.quiet = now, 0
		return false
	}
	a.quiet++
	return a.quiet >= appIdleTicks
}

// startLinking runs one round of pairing and fetching, unless one is running.
func (a *appPage) startLinking() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.linking {
		return
	}
	page := pairpage.New(a.storeDir, pairPageCommand(a.flags, a.storeDir), a.lang)
	if err := page.Start(); err != nil {
		a.failure = err.Error()
		return
	}
	a.page, a.linking, a.failure = page, true, ""
	a.linkingDone.Add(1)
	go func() {
		defer a.linkingDone.Done()
		err := a.link(page)
		// A round that ended before it could say so still ends on the page.
		_ = page.Finish(0, err)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.linking = false
		if err != nil {
			a.failure = err.Error()
		}
	}()
}

func (a *appPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The name a browser asked for must be this computer's own: a page
	// elsewhere that points a name of its own at 127.0.0.1 is turned away.
	if r.Host != a.host {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	token, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	a.requests.Add(1)
	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")

	switch r.Method + " " + path {
	case "GET ":
		a.servePage(w, r)
	case "GET pair.js":
		a.serveScript(w)
	case "GET status":
		a.serveStatus(w, r, r.URL.Query().Get("receiver") != "")
	case "GET prompt":
		answerIn := ""
		if a.language(r) == pairpage.Dutch {
			answerIn = "Dutch"
		}
		writeAppJSON(w, map[string]any{"prompt": push.ReceiverPrompt(answerIn)})
	case "POST on", "POST off", "POST link":
		// A change comes from this page and from no other.
		if r.Header.Get("Origin") != "http://"+a.host || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		a.serveChange(w, r, path)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (a *appPage) language(r *http.Request) string {
	if a.lang != "" {
		return pairpage.Language(a.lang)
	}
	first, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	first, _, _ = strings.Cut(first, ";")
	return pairpage.Language(first)
}

func (a *appPage) current() *pairpage.Page {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.page
}

func (a *appPage) servePage(w http.ResponseWriter, r *http.Request) {
	page := a.current()
	if page == nil {
		// The first round could not write its page: say why in plain text.
		a.mu.Lock()
		failure := a.failure
		a.mu.Unlock()
		http.Error(w, "wacli could not start: "+failure, http.StatusInternalServerError)
		return
	}
	html, err := page.Served(a.language(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

func (a *appPage) serveScript(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	page := a.current()
	if page == nil {
		return
	}
	if script, err := page.Script(); err == nil {
		_, _ = w.Write(script)
	}
}

// serveStatus answers what the panel draws: `push status`, and whether
// pairing is running. asked says the receiver was asked too.
func (a *appPage) serveStatus(w http.ResponseWriter, r *http.Request, receiver bool) {
	args := []string{"push", "status"}
	if receiver {
		args = append(args, "--receiver")
	}
	status, err := a.wacli(r.Context(), "", args...)
	a.writeStatus(w, status, err, receiver)
}

func (a *appPage) serveChange(w http.ResponseWriter, r *http.Request, what string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeAppJSON(w, map[string]any{"error": err.Error()})
		return
	}
	var status json.RawMessage
	switch what {
	case "on":
		// The address and the key go over standard input: a program's
		// arguments can be read by every other program on the computer.
		status, err = a.wacli(r.Context(), string(body), "push", "install", "--stdin")
	case "off":
		status, err = a.wacli(r.Context(), "", "push", "uninstall")
	case "link":
		a.startLinking()
		status, err = a.wacli(r.Context(), "", "push", "status")
	}
	a.writeStatus(w, status, err, false)
}

func (a *appPage) writeStatus(w http.ResponseWriter, status json.RawMessage, err error, asked bool) {
	if err != nil {
		writeAppJSON(w, map[string]any{"error": err.Error()})
		return
	}
	a.mu.Lock()
	linking := map[string]any{"running": a.linking, "failure": a.failure}
	a.mu.Unlock()
	writeAppJSON(w, map[string]any{"push": status, "linking": linking, "asked": asked})
}

func writeAppJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// runWacli runs this program for this store with --json and returns the data
// it printed. A failure carries the message wacli gave.
func (a *appPage) runWacli(ctx context.Context, input string, args ...string) (json.RawMessage, error) {
	program, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find this program: %w", err)
	}
	cmd := exec.CommandContext(ctx, program, append([]string{"--store", a.storeDir, "--json"}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *string         `json:"error"`
	}
	if runErr == nil {
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.Data == nil {
			return nil, errors.New("wacli answered something this page cannot read")
		}
		return envelope.Data, nil
	}
	// A failure is the same envelope on stderr, on its last line.
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	last := lines[len(lines)-1]
	if json.Unmarshal([]byte(last), &envelope) == nil && envelope.Error != nil {
		return nil, errors.New(*envelope.Error)
	}
	if last == "" {
		last = runErr.Error()
	}
	return nil, errors.New(last)
}
