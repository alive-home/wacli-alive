package push

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/openclaw/wacli/internal/safehttp"
	"github.com/openclaw/wacli/internal/store"
)

const (
	schemaVersion = 1
	maxItems      = 200
	maxItemBytes  = 256 << 10 // per request; one larger item goes alone
)

// Message is the fixed set of message fields that leaves the store. Local file
// paths and the fields that fetch an attachment from WhatsApp are not in it.
type Message struct {
	ChatJID       string    `json:"chat_jid"`
	ChatName      string    `json:"chat_name,omitempty"`
	MsgID         string    `json:"msg_id"`
	Timestamp     time.Time `json:"timestamp"`
	FromMe        bool      `json:"from_me"`
	SenderJID     string    `json:"sender_jid,omitempty"`
	SenderName    string    `json:"sender_name,omitempty"`
	Text          string    `json:"text"`
	DisplayText   string    `json:"display_text,omitempty"`
	QuotedMsgID   string    `json:"quoted_msg_id,omitempty"`
	Forwarded     bool      `json:"forwarded,omitempty"`
	ReactionToID  string    `json:"reaction_to_id,omitempty"`
	ReactionEmoji string    `json:"reaction_emoji,omitempty"`
	MediaType     string    `json:"media_type,omitempty"`
	MediaCaption  string    `json:"media_caption,omitempty"`
	Filename      string    `json:"filename,omitempty"`
	MimeType      string    `json:"mime_type,omitempty"`
	Edited        bool      `json:"edited,omitempty"`
}

// Removed names a message the store no longer shows: revoked by its sender,
// deleted, or moved to another id.
type Removed struct {
	ChatJID        string `json:"chat_jid"`
	MsgID          string `json:"msg_id"`
	Reason         string `json:"reason,omitempty"`
	MovedToChatJID string `json:"moved_to_chat_jid,omitempty"`
	MovedToMsgID   string `json:"moved_to_msg_id,omitempty"`
}

func wireMessage(m store.Message) Message {
	w := Message{
		ChatJID: m.ChatJID, ChatName: m.ChatName, MsgID: m.MsgID, Timestamp: m.Timestamp,
		FromMe: m.FromMe, SenderJID: m.SenderJID, SenderName: m.SenderName, Text: m.Text,
		QuotedMsgID: m.QuotedMsgID, Forwarded: m.IsForwarded,
		ReactionToID: m.ReactionToID, ReactionEmoji: m.ReactionEmoji,
		MediaType: m.MediaType, MediaCaption: m.MediaCaption, Filename: m.Filename, MimeType: m.MimeType,
		Edited: m.Edited,
	}
	if m.DisplayText != m.Text {
		w.DisplayText = m.DisplayText
	}
	return w
}

type batch struct {
	messages []Message
	removed  []Removed
	calls    []store.CallEvent
}

type envelope struct {
	Schema       int               `json:"schema"`
	StoreID      string            `json:"store_id"`
	SentAt       time.Time         `json:"sent_at"`
	WacliVersion string            `json:"wacli_version"`
	OS           string            `json:"os"`
	Linked       bool              `json:"linked"`
	LastSyncAt   *time.Time        `json:"last_sync_at"`
	More         bool              `json:"more"`
	Messages     []json.RawMessage `json:"messages"`
	Removed      []json.RawMessage `json:"removed"`
	Calls        []json.RawMessage `json:"calls"`
}

type poster struct {
	client   *http.Client
	url      string
	key      []byte
	version  string
	linked   bool
	storeID  string
	lastSync *time.Time
}

func newPoster(opts Options) (*poster, error) {
	u, err := url.Parse(opts.URL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("push: invalid URL")
	}
	// A receiver on this computer needs neither https nor the flag: nothing
	// crosses a network.
	local := thisComputer(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (opts.AllowPrivate || local)) {
		return nil, fmt.Errorf("push: the URL must use https (http only for a receiver on this computer, or with --allow-private)")
	}
	client := safehttp.NewClient()
	if opts.AllowPrivate || local {
		// As above, the content goes where the URL says and nowhere else:
		// a proxy from the environment is not used.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		if !opts.AllowPrivate {
			transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: onlyThisComputer}).DialContext
		}
		client = &http.Client{Transport: transport}
	}
	client.Timeout = 30 * time.Second
	// A redirect is never followed: the content goes to the configured host only.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &poster{client: client, url: opts.URL, key: opts.Key, version: opts.Version, linked: opts.Linked}, nil
}

// thisComputer reports whether host names this computer itself.
func thisComputer(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// onlyThisComputer refuses a connection that leaves this computer, for a name
// like localhost that resolves somewhere else.
func onlyThisComputer(_, address string, _ syscall.RawConn) error {
	if host, _, err := net.SplitHostPort(address); err != nil || !thisComputer(host) {
		return fmt.Errorf("the address does not resolve to this computer")
	}
	return nil
}

// requests splits a batch into requests of at most maxItems items and about
// maxItemBytes of items. Only the run's closing request has "more": false, and
// that one is made even when the batch is empty.
func (p *poster) requests(b batch, closing bool) ([]envelope, error) {
	type item struct {
		list int // 0 messages, 1 removed, 2 calls
		v    any
	}
	items := make([]item, 0, len(b.messages)+len(b.removed)+len(b.calls))
	for _, m := range b.messages {
		items = append(items, item{0, m})
	}
	for _, r := range b.removed {
		items = append(items, item{1, r})
	}
	for _, c := range b.calls {
		items = append(items, item{2, c})
	}

	var out []envelope
	env := p.newEnvelope()
	count, size := 0, 0
	flush := func(more bool) {
		env.More = more
		out = append(out, env)
		env, count, size = p.newEnvelope(), 0, 0
	}
	for _, it := range items {
		raw, err := json.Marshal(it.v)
		if err != nil {
			return nil, err
		}
		if count > 0 && (count == maxItems || size+len(raw) > maxItemBytes) {
			flush(true)
		}
		lists := [...]*[]json.RawMessage{&env.Messages, &env.Removed, &env.Calls}
		*lists[it.list] = append(*lists[it.list], raw)
		count++
		size += len(raw)
	}
	if count > 0 || closing {
		flush(!closing)
	}
	return out, nil
}

func (p *poster) newEnvelope() envelope {
	return envelope{
		Schema: schemaVersion, StoreID: p.storeID,
		WacliVersion: p.version, OS: runtime.GOOS, Linked: p.linked, LastSyncAt: p.lastSync,
		Messages: []json.RawMessage{}, Removed: []json.RawMessage{}, Calls: []json.RawMessage{},
	}
}

// post sends one request. sent_at is the moment it leaves: the receiver
// refuses a request that is minutes old.
func (p *poster) post(ctx context.Context, env envelope) error {
	env.SentAt = time.Now().UTC().Truncate(time.Second)
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: create request: invalid URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wacli/"+p.version)
	if len(p.key) > 0 {
		req.Header.Set("X-Wacli-Key-Id", keyID(p.key))
		req.Header.Set("X-Wacli-Signature", signature(p.key, body))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// The URL can hold a token in its path or query: name the host and
		// the cause, never the URL the client's own error carries.
		var withURL *url.Error
		if errors.As(err, &withURL) {
			err = withURL.Err
		}
		return fmt.Errorf("push: request to %s failed: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push: %s answered %d", req.URL.Host, resp.StatusCode)
	}
	return nil
}

// keyID tells the receiver which key signed a request without showing the
// key: the first 16 hex characters of its SHA-256.
func keyID(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:8])
}

// signature is the X-Wacli-Signature value: HMAC-SHA256 of the request body
// under the key, the form the sync webhook signs with.
func signature(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
