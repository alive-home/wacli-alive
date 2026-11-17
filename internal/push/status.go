package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/openclaw/wacli/internal/store"
)

// ReceiverStatus is what a receiver says about the person of the key: counters
// it keeps itself. Every field is a time or a number, so nothing a receiver
// answers is text this computer shows or acts on.
type ReceiverStatus struct {
	// LastReadAt is when that person's read key last read; nil when it never did.
	LastReadAt *time.Time `json:"last_read_at"`
	// ReadsToday counts its read requests on the current UTC day.
	ReadsToday int `json:"reads_today"`
	// Messages and Calls count the rows the receiver holds for that person.
	Messages int `json:"messages"`
	Calls    int `json:"calls"`
}

// ErrNoStatus is a receiver that takes the request and answers no status: one
// built before wacli asked for it.
var ErrNoStatus = errors.New("push: the receiver does not answer a status request")

// The answer is four short fields; anything longer is not one.
const maxStatusBytes = 4 << 10

// AskStatus posts one signed request without content that asks the receiver
// for its counters of this key's person. It is the only request whose answer
// is read, and only the fields of ReceiverStatus are taken from it. No
// position moves.
func AskStatus(ctx context.Context, db *store.DB, opts Options) (ReceiverStatus, error) {
	post, _, err := open(db, opts)
	if err != nil {
		return ReceiverStatus{}, err
	}
	env := post.newEnvelope()
	// A receiver that does not know the request takes it for the check
	// `push install` sends: it must not read it as "caught up".
	env.More, env.Status = true, true
	got, err := post.send(ctx, env, maxStatusBytes)
	if err != nil {
		return ReceiverStatus{}, err
	}
	if got.status != http.StatusOK || len(bytes.TrimSpace(got.body)) == 0 {
		return ReceiverStatus{}, ErrNoStatus
	}
	if len(got.body) > maxStatusBytes {
		return ReceiverStatus{}, fmt.Errorf("push: the status %s answered is longer than %d bytes", got.host, maxStatusBytes)
	}
	// An answer without the four fields is some other 200, not a status of zeros.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &fields); err != nil {
		return ReceiverStatus{}, fmt.Errorf("push: the status %s answered cannot be read", got.host)
	}
	for _, name := range []string{"last_read_at", "reads_today", "messages", "calls"} {
		if _, ok := fields[name]; !ok {
			return ReceiverStatus{}, ErrNoStatus
		}
	}
	var status ReceiverStatus
	if err := json.Unmarshal(got.body, &status); err != nil {
		return ReceiverStatus{}, fmt.Errorf("push: the status %s answered cannot be read", got.host)
	}
	if status.ReadsToday < 0 || status.Messages < 0 || status.Calls < 0 {
		return ReceiverStatus{}, fmt.Errorf("push: the status %s answered has a negative count", got.host)
	}
	if status.LastReadAt != nil {
		// A whole second in UTC, whatever precision and zone the receiver wrote:
		// the programs that show it read one shape of time.
		at := status.LastReadAt.UTC().Truncate(time.Second)
		status.LastReadAt = &at
	}
	return status, nil
}
