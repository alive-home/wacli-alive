package main

import "github.com/spf13/cobra"

func newGroupsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Group management",
	}
	cmd.AddCommand(newGroupsListCmd(flags))
	cmd.AddCommand(newGroupsRefreshCmd(flags))
	cmd.AddCommand(newGroupsInfoCmd(flags))
	cmd.AddCommand(newGroupsParticipantsCmd(flags))
	cmd.AddCommand(newGroupsRequestsCmd(flags))
	cmd.AddCommand(newGroupsInviteCmd(flags))
	cmd.AddCommand(newGroupsPruneCmd(flags))
	return cmd
}
