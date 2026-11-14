package wa

import (
	"context"
	"fmt"
)

// HasLocalAppState reports whether whatsmeow has any local state for the
// given app state collection (a non-zero synced version). Used to decide
// whether settling replay debt with an incremental fetch is safe.
func (c *Client) HasLocalAppState(ctx context.Context, name string) (bool, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || cli.Store == nil || cli.Store.AppState == nil {
		return false, fmt.Errorf("not connected")
	}
	version, _, err := cli.Store.AppState.GetAppStateVersion(ctx, name)
	if err != nil {
		return false, err
	}
	return version > 0, nil
}
