package wa

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func (c *Client) GetProfilePictureInfo(ctx context.Context, jid types.JID, preview bool, existingID string) (*types.ProfilePictureInfo, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{
		Preview:    preview,
		ExistingID: existingID,
	})
}

func (c *Client) GetBusinessProfile(ctx context.Context, jid types.JID) (*types.BusinessProfile, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetBusinessProfile(ctx, jid)
}
