package wa

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type pollVoteLIDResolver func(context.Context, *whatsmeow.Client, types.JID) types.JID

// DecryptPollVote decrypts an incoming PollUpdateMessage event and returns
// the SHA-256 hashes of the selected options. The caller is responsible for
// matching those hashes back to option names.
func (c *Client) DecryptPollVote(ctx context.Context, evt *events.Message) (*waE2E.PollVoteMessage, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil {
		return nil, fmt.Errorf("whatsapp client is not initialized")
	}
	return cli.DecryptPollVote(ctx, evt)
}
