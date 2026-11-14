package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/out"
	"github.com/spf13/cobra"
	"go.mau.fi/whatsmeow/types"
)

func newGroupsRequestsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "Manage group join requests",
	}
	cmd.AddCommand(newGroupsRequestsListCmd(flags))
	return cmd
}

// requestListEntry is the JSON/text output shape for a single pending join request.
type requestListEntry struct {
	JID         string    `json:"JID"`
	PhoneNumber string    `json:"phone_number,omitempty"`
	RequestedAt time.Time `json:"RequestedAt"`
}

// resolveRequestEntries converts raw join-request participants to output entries,
// resolving LID JIDs to phone numbers via the provided resolver function.
func resolveRequestEntries(ctx context.Context, requests []types.GroupParticipantRequest, resolve func(context.Context, types.JID) types.JID) []requestListEntry {
	entries := make([]requestListEntry, len(requests))
	for i, req := range requests {
		pn := resolve(ctx, req.JID)
		phone := ""
		if pn.Server == types.DefaultUserServer {
			phone = "+" + pn.User
		}
		entries[i] = requestListEntry{
			JID:         req.JID.String(),
			PhoneNumber: phone,
			RequestedAt: req.RequestedAt,
		}
	}
	return entries
}

func newGroupsRequestsListCmd(flags *rootFlags) *cobra.Command {
	var jidStr string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending group join requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(jidStr) == "" {
				return fmt.Errorf("--jid is required")
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			a, lk, err := newApp(ctx, flags, true, false)
			if err != nil {
				return err
			}
			defer closeApp(a, lk)

			if err := a.EnsureAuthed(ctx); err != nil {
				return err
			}
			if err := a.Connect(ctx, false, nil); err != nil {
				return err
			}
			gjid, err := parseGroupJID(jidStr)
			if err != nil {
				return err
			}
			requests, err := a.WA().GetGroupRequestParticipants(ctx, gjid)
			if err != nil {
				return err
			}

			entries := resolveRequestEntries(ctx, requests, a.WA().ResolveLIDToPN)

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, entries)
			}
			for _, e := range entries {
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", e.JID, e.RequestedAt.Local().Format("2006-01-02 15:04:05"), e.PhoneNumber)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&jidStr, "jid", "", "group JID (…@g.us)")
	return cmd
}

func parseGroupJID(raw string) (types.JID, error) {
	jid, err := types.ParseJID(strings.TrimSpace(raw))
	if err != nil {
		return types.JID{}, err
	}
	if jid.Server != types.GroupServer {
		return types.JID{}, fmt.Errorf("expected group JID ending in @g.us, got %s", jid.String())
	}
	return jid, nil
}
