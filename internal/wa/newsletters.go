package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func (c *Client) GetNewsletterInfoWithInvite(ctx context.Context, key string) (*types.NewsletterMetadata, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetNewsletterInfoWithInvite(ctx, key)
}

func (c *Client) GetSubscribedNewsletters(ctx context.Context) ([]*types.NewsletterMetadata, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetSubscribedNewsletters(ctx)
}

func (c *Client) GetNewsletterInfo(ctx context.Context, jid types.JID) (*types.NewsletterMetadata, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetNewsletterInfo(ctx, jid)
}

func NewsletterName(meta *types.NewsletterMetadata) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(meta.ThreadMeta.Name.Text)
}
