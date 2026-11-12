// Package pairpage shows WhatsApp pairing on a local web page, for people who
// would rather not read a QR code from a terminal.
//
// The page is two files in the store directory: pair.html, which a browser
// opens from disk, and pair.js, which the open page loads again every moment
// to follow the pairing. Nothing is served and the page loads nothing from the
// network.
package pairpage

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/openclaw/wacli/internal/fsutil"
	"rsc.io/qr"
)

// SourceURL is where the page says the code can be read.
const SourceURL = "https://github.com/alive-home/wacli-alive"

const (
	htmlName = "pair.html"
	jsName   = "pair.js"

	// A live page that has heard nothing for staleAfter says pairing stopped,
	// so a killed process does not leave a dead code on screen.
	staleAfter = 60 * time.Second
	heartbeat  = 20 * time.Second

	// Modules of white around the code, which scanners need.
	quietZone = 4
)

// The states the page draws. page.html uses the same names.
const (
	stateStarting = "starting" // waiting for the first code
	stateScan     = "scan"     // a code is on screen
	stateSyncing  = "syncing"  // linked, messages are being fetched
	stateDone     = "done"     // linked, fetching finished
	stateHalted   = "halted"   // linked, fetching stopped early
	stateStopped  = "stopped"  // not linked, no more codes
)

//go:embed page.html
var pageHTML string

var pageTemplate = template.Must(template.New("page").Parse(pageHTML))

type view struct {
	Seq      int64  `json:"seq"`
	State    string `json:"state"`
	QR       string `json:"qr,omitempty"`
	QRSize   int    `json:"qrSize,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Messages int64  `json:"messages"`
	Until    int64  `json:"until,omitempty"`
}

func (v view) live() bool {
	return v.State == stateStarting || v.State == stateScan || v.State == stateSyncing
}

// Page writes the pairing page as pairing moves along.
type Page struct {
	storeDir  string
	storeFlag string
	now       func() time.Time

	mu     sync.Mutex
	cur    view
	linked bool
	closed bool
	stop   chan struct{}
}

// New prepares a page in storeDir. storeFlag is what a command needs to reach
// this store (`--store "…"`), or empty when a bare `wacli` already does.
func New(storeDir, storeFlag string) *Page {
	return &Page{storeDir: storeDir, storeFlag: storeFlag, now: time.Now, stop: make(chan struct{})}
}

// Path is the file a browser opens.
func (p *Page) Path() string { return filepath.Join(p.storeDir, htmlName) }

// Start writes the page before any code exists and keeps it marked as alive
// until Finish.
func (p *Page) Start() error {
	if err := p.set(func(v *view) { v.State = stateStarting }); err != nil {
		return err
	}
	go p.keepAlive()
	return nil
}

// ShowQR puts a pairing code on the page. WhatsApp replaces the code every
// few seconds, so this is called again for each one.
func (p *Page) ShowQR(code string) error {
	path, size, err := qrPath(code)
	if err != nil {
		return err
	}
	return p.set(func(v *view) { v.State, v.QR, v.QRSize = stateScan, path, size })
}

// Linked says pairing worked. phone is the linked number in digits.
func (p *Page) Linked(phone string) error {
	return p.set(func(v *view) {
		p.linked = true
		*v = view{State: stateSyncing, Phone: phone}
	})
}

// Finish writes how it ended: what was fetched, that fetching stopped early,
// or that nothing was linked.
func (p *Page) Finish(messages int64, syncErr error) error {
	err := p.set(func(v *view) {
		switch {
		case !p.linked:
			*v = view{State: stateStopped}
		case syncErr != nil:
			*v = view{State: stateHalted, Phone: v.Phone, Messages: messages}
		default:
			*v = view{State: stateDone, Phone: v.Phone, Messages: messages}
		}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.stop)
	}
	return err
}

func (p *Page) keepAlive() {
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			_ = p.set(func(*view) {})
		}
	}
}

func (p *Page) set(change func(*view)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	change(&p.cur)
	now := p.now()
	p.cur.Seq = now.UnixMilli()
	p.cur.Until = 0
	if p.cur.live() {
		p.cur.Until = now.Add(staleAfter).UnixMilli()
	}

	html, err := renderHTML(p.cur, p.storeDir, p.storeFlag)
	if err != nil {
		return err
	}
	state, err := json.Marshal(p.cur)
	if err != nil {
		return err
	}
	// The script first: a page that is opened now must find it.
	if err := fsutil.WritePrivateFileAtomic(filepath.Join(p.storeDir, jsName), []byte("pairState("+string(state)+");\n")); err != nil {
		return fmt.Errorf("write pairing page: %w", err)
	}
	if err := fsutil.WritePrivateFileAtomic(p.Path(), html); err != nil {
		return fmt.Errorf("write pairing page: %w", err)
	}
	return nil
}

func renderHTML(v view, storeDir, storeFlag string) ([]byte, error) {
	var buf bytes.Buffer
	err := pageTemplate.Execute(&buf, map[string]any{
		"View":     v,
		"Live":     v.live(),
		"StoreDir": storeDir,
		"Source":   SourceURL,
		"SourceID": strings.TrimPrefix(SourceURL, "https://"),
		"PromptNL": Prompt("nl", storeDir, storeFlag),
		"PromptEN": Prompt("en", storeDir, storeFlag),
		"Script":   jsName,
	})
	if err != nil {
		return nil, fmt.Errorf("render pairing page: %w", err)
	}
	return buf.Bytes(), nil
}

// qrPath draws the code as SVG path data, one rectangle per run of dark
// modules, and returns the width of the drawing in modules.
func qrPath(code string) (string, int, error) {
	c, err := qr.Encode(code, qr.M)
	if err != nil {
		return "", 0, fmt.Errorf("encode QR code: %w", err)
	}
	var b strings.Builder
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if !c.Black(x, y) {
				continue
			}
			start := x
			for x < c.Size && c.Black(x, y) {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", start+quietZone, y+quietZone, x-start, x-start)
		}
	}
	return b.String(), c.Size + 2*quietZone, nil
}

// PromptCommands are the commands the prompt teaches, with what each is for.
// A test in cmd/wacli checks that this build has every one of them.
var PromptCommands = []struct{ Command, NL, EN string }{
	{`sync --once`, "nieuwe berichten ophalen", "fetch new messages"},
	{`chats list`, "mijn gesprekken tonen", "list my chats"},
	{`messages search "tekst"`, "in alle berichten zoeken", "search all messages"},
	{`messages list --chat "naam"`, "één gesprek lezen", "read one chat"},
	{`--help`, "alle opdrachten", "every command"},
}

var englishPlaceholders = strings.NewReplacer(`"tekst"`, `"text"`, `"naam"`, `"name"`)

// Prompt is the text a person pastes into their AI assistant so it knows what
// this link is and how to read from it.
func Prompt(lang, storeDir, storeFlag string) string {
	wacli := "wacli"
	if storeFlag != "" {
		wacli += " " + storeFlag
	}
	var b strings.Builder
	if lang == "nl" {
		b.WriteString("Mijn WhatsApp is op deze computer gekoppeld met wacli. Het kan alleen lezen, niet versturen.\n\nOpdrachten:\n")
	} else {
		b.WriteString("My WhatsApp is linked on this computer with wacli. It can only read, not send.\n\nCommands:\n")
	}
	for _, c := range PromptCommands {
		use, command := c.EN, englishPlaceholders.Replace(c.Command)
		if lang == "nl" {
			use, command = c.NL, c.Command
		}
		fmt.Fprintf(&b, "- %s %s: %s\n", wacli, command, use)
	}
	if lang == "nl" {
		fmt.Fprintf(&b, "\nMet --json krijg je uitvoer die je kunt verwerken. De berichten staan in %s.\nNiet meer gekoppeld? Start `%s auth --open`; ik scan de code.", storeDir, wacli)
	} else {
		fmt.Fprintf(&b, "\nAdd --json for output you can parse. The messages are in %s.\nNo longer linked? Run `%s auth --open` and I will scan the code.", storeDir, wacli)
	}
	return b.String()
}

// Open shows path in the default browser.
func Open(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
