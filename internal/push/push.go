// Package push posts what the local store holds to a URL the user configured:
// every message once, then each change, and the call events. It only posts.
// Nothing in a response is read beyond the status code, so the receiving side
// cannot ask this computer for anything.
package push

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openclaw/wacli/internal/fsutil"
	"github.com/openclaw/wacli/internal/store"
)

const (
	pageSize = 200
	// An outcome or duration that reaches a call event later is sent again
	// when the event is at most this old.
	callOverlap = 24 * time.Hour
)

type Options struct {
	URL          string
	Key          []byte // signs each request; empty sends unsigned
	AllowPrivate bool   // allow http and addresses that are not public
	DryRun       bool   // count what would go; post nothing, keep the position
	StoreDir     string // where the position is kept
	Version      string
	Linked       bool // the store still has its WhatsApp session
	// ResolveNames fills sender names the store has no name for.
	ResolveNames func([]store.Message) []store.Message
}

type Result struct {
	Requests  int  `json:"requests"`
	Messages  int  `json:"messages"`
	Removed   int  `json:"removed"`
	Calls     int  `json:"calls"`
	Restarted bool `json:"restarted,omitempty"`
}

// position is how far one destination has been served from one store.
type position struct {
	StoreID    string    `json:"store_id"`
	Row        int64     `json:"row"`        // last message row of the full copy that arrived
	CopyDone   bool      `json:"copy_done"`  // the full copy is complete; changes are followed from Checkpoint
	Checkpoint string    `json:"checkpoint"` // place in the store's change journal
	CallsRow   int64     `json:"calls_row"`
	CallsAt    time.Time `json:"calls_at"`
}

type run struct {
	db   *store.DB
	opts Options
	post *poster
	pos  position
	path string
	res  Result
}

// Check posts one request without content that says more will follow. A
// receiver that answers 2xx takes this URL and key; it is not told that the
// store is caught up, and no position moves.
func Check(ctx context.Context, db *store.DB, opts Options) error {
	post, _, err := open(db, opts)
	if err != nil {
		return err
	}
	env := post.newEnvelope()
	env.More = true
	return post.post(ctx, env)
}

// open makes the poster for this store and reads where its journal stands.
func open(db *store.DB, opts Options) (*poster, store.ChangesPage, error) {
	post, err := newPoster(opts)
	if err != nil {
		return nil, store.ChangesPage{}, err
	}
	now, err := db.MessageChanges("", true, 0)
	if err != nil {
		return nil, store.ChangesPage{}, err
	}
	post.storeID = now.StoreID
	if post.lastSync, err = db.LastSuccessfulSync(); err != nil {
		return nil, store.ChangesPage{}, err
	}
	return post, now, nil
}

// Run sends everything the destination has not had yet and ends with one
// closing request, also when there was nothing to send: that request is the
// sign of life.
func Run(ctx context.Context, db *store.DB, opts Options) (Result, error) {
	post, now, err := open(db, opts)
	if err != nil {
		return Result{}, err
	}
	r := &run{db: db, opts: opts, post: post, path: positionPath(opts.StoreDir, opts.URL)}
	if err := r.loadPosition(now); err != nil {
		return Result{}, err
	}
	err = r.messages(ctx)
	if errors.Is(err, store.ErrCheckpointExpired) {
		// The journal no longer reaches back to our place: copy everything
		// again and follow it from today. The receiver stores by message id.
		r.pos = position{StoreID: now.StoreID, Checkpoint: now.NextCheckpoint, CallsRow: r.pos.CallsRow, CallsAt: r.pos.CallsAt}
		r.res.Restarted = true
		err = r.messages(ctx)
	}
	if err != nil {
		return r.res, err
	}
	return r.res, r.calls(ctx)
}

func (r *run) messages(ctx context.Context) error {
	for !r.pos.CopyDone {
		msgs, last, err := r.db.MessagesAfterRow(r.pos.Row, pageSize)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			r.pos.CopyDone = true
		} else if err := r.send(ctx, r.current(msgs), false); err != nil {
			return err
		}
		r.pos.Row = last
		if err := r.savePosition(); err != nil {
			return err
		}
	}
	for {
		page, err := r.db.MessageChanges(r.pos.Checkpoint, false, pageSize)
		if err != nil {
			return err
		}
		b, err := r.changed(page.Changes)
		if err != nil {
			return err
		}
		if err := r.send(ctx, b, false); err != nil {
			return err
		}
		r.pos.Checkpoint = page.NextCheckpoint
		if err := r.savePosition(); err != nil {
			return err
		}
		if !page.HasMore {
			return nil
		}
	}
}

// changed turns a page of the journal into what the store holds now for each
// message it names: the message as it is, or a removal when it is gone.
func (r *run) changed(changes []store.MessageChange) (batch, error) {
	type key struct{ chat, id string }
	last := map[key]store.MessageChange{}
	var order []key
	touch := func(k key, c store.MessageChange) {
		if _, seen := last[k]; !seen {
			order = append(order, k)
		}
		last[k] = c
	}
	for _, c := range changes {
		// A chat that moves to another id keeps its message ids.
		if c.NewChatJID != "" && c.NewMsgID == "" {
			c.NewMsgID = c.MsgID
		}
		touch(key{c.ChatJID, c.MsgID}, c)
		if c.NewMsgID != "" {
			touch(key{c.NewChatJID, c.NewMsgID}, store.MessageChange{Operation: "upsert"})
		}
	}
	var gone []Removed
	var found []store.Message
	for _, k := range order {
		m, err := r.db.GetMessage(k.chat, k.id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			c := last[k]
			gone = append(gone, Removed{ChatJID: k.chat, MsgID: k.id, Reason: c.Reason, MovedToChatJID: c.NewChatJID, MovedToMsgID: c.NewMsgID})
		case err != nil:
			return batch{}, err
		default:
			found = append(found, m)
		}
	}
	b := r.current(found)
	b.removed = append(gone, b.removed...)
	return b, nil
}

// current sorts stored rows into what goes out: a message as it is now, or
// only its removal when it was deleted or revoked. A removal carries no text.
func (r *run) current(rows []store.Message) batch {
	var b batch
	var shown []store.Message
	for _, m := range rows {
		if m.DeletedAt != nil || m.Revoked {
			b.removed = append(b.removed, Removed{ChatJID: m.ChatJID, MsgID: m.MsgID, Reason: m.DeletionReason})
		} else {
			shown = append(shown, m)
		}
	}
	if r.opts.ResolveNames != nil {
		shown = r.opts.ResolveNames(shown)
	}
	b.messages = make([]Message, len(shown))
	for i, m := range shown {
		b.messages[i] = wireMessage(m)
	}
	return b
}

func (r *run) calls(ctx context.Context) error {
	started := time.Now().UTC()
	calls, last, err := r.db.CallEventsAfterRow(r.pos.CallsRow, r.pos.CallsAt.Add(-callOverlap))
	if err != nil {
		return err
	}
	if err := r.send(ctx, batch{calls: calls}, true); err != nil {
		return err
	}
	r.pos.CallsRow, r.pos.CallsAt = last, started
	return r.savePosition()
}

// send posts a batch in as many requests as its size needs. closing marks the
// last request of the run and is sent even when the batch is empty.
func (r *run) send(ctx context.Context, b batch, closing bool) error {
	requests, err := r.post.requests(b, closing)
	if err != nil {
		return err
	}
	for _, req := range requests {
		if !r.opts.DryRun {
			if err := r.post.post(ctx, req); err != nil {
				return err
			}
		}
		r.res.Requests++
	}
	r.res.Messages += len(b.messages)
	r.res.Removed += len(b.removed)
	r.res.Calls += len(b.calls)
	return nil
}

func positionPath(storeDir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(storeDir, "push-"+hex.EncodeToString(sum[:8])+".json")
}

// loadPosition reads where this destination is. A destination that is new, or
// whose position is from another store, starts with the full copy and follows
// the journal from where it stands now.
func (r *run) loadPosition(now store.ChangesPage) error {
	data, err := os.ReadFile(r.path)
	if err == nil {
		if err := json.Unmarshal(data, &r.pos); err != nil {
			return fmt.Errorf("read push position %s: %w", r.path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.pos.StoreID != now.StoreID {
		r.pos = position{StoreID: now.StoreID, Checkpoint: now.NextCheckpoint}
	}
	return nil
}

func (r *run) savePosition() error {
	if r.opts.DryRun {
		return nil
	}
	data, err := json.Marshal(r.pos)
	if err != nil {
		return err
	}
	return fsutil.WritePrivateFileAtomic(r.path, data)
}
