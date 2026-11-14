package wa

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types"
)

func (c *Client) GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetJoinedGroups(ctx)
}

type GroupParticipantAction string

const (
	GroupParticipantAdd     GroupParticipantAction = "add"
	GroupParticipantRemove  GroupParticipantAction = "remove"
	GroupParticipantPromote GroupParticipantAction = "promote"
	GroupParticipantDemote  GroupParticipantAction = "demote"
)

type GroupParticipantRequestAction string

const (
	GroupParticipantRequestApprove GroupParticipantRequestAction = "approve"
	GroupParticipantRequestReject  GroupParticipantRequestAction = "reject"
)

func (c *Client) GetGroupRequestParticipants(ctx context.Context, group types.JID) ([]types.GroupParticipantRequest, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return cli.GetGroupRequestParticipants(ctx, group)
}

func (c *Client) GetGroupInviteLink(ctx context.Context, group types.JID) (string, error) {
	c.mu.Lock()
	cli := c.client
	c.mu.Unlock()
	if cli == nil || !cli.IsConnected() {
		return "", fmt.Errorf("not connected")
	}
	return cli.GetGroupInviteLink(ctx, group, false)
}
