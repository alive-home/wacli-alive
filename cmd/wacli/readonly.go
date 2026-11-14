package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/wa"
	"github.com/spf13/cobra"
)

// This file is the command-line half of the read-only build. The other half
// is that wacli's code that sends to or changes WhatsApp is deleted: WAClient
// in internal/app has no method for it, and the guard in
// internal/app/guard.go keeps a new one from slipping in.

var errReadOnlyBuild = errors.New("wacli read-only build: this binary cannot send to or change WhatsApp")

// readOnlyCommands is every command this build runs. Whatever else is in the
// command tree is cut out, so a command upstream adds stays out until it is
// listed here. List only commands that read from WhatsApp or work on the local
// store.
var readOnlyCommands = []string{
	"accounts add", "accounts list", "accounts remove", "accounts show", "accounts use",
	"auth", "auth logout", "auth status",
	"calls list",
	"channels info", "channels list",
	"chats cleanup", "chats list", "chats show",
	"contacts alias rm", "contacts alias set", "contacts check", "contacts import-system",
	"contacts refresh", "contacts resolve", "contacts search", "contacts show",
	"contacts tags add", "contacts tags rm",
	"doctor",
	"groups info", "groups invite link get", "groups list", "groups participants list",
	"groups prune", "groups refresh", "groups requests list",
	"history backfill", "history coverage", "history fill",
	"media backfill", "media download", "media retry",
	"messages changes", "messages context", "messages export", "messages list",
	"messages purge", "messages search", "messages show", "messages starred",
	"poll show", "polls list",
	"profile business", "profile get-about", "profile picture-info",
	"store cleanup", "store stats",
	"sync",
	"version",
}

// removedCommands are upstream's commands that send to or change WhatsApp.
// Their code is not in this build. The names stay, each as a stub that says so,
// so a command line written for upstream fails with the reason instead of
// "unknown command".
var removedCommands = []string{
	"channels join", "channels leave", "chats archive", "chats mark-read", "chats mark-unread",
	"chats mute", "chats pin", "chats unarchive", "chats unmute", "chats unpin",
	"contacts save", "groups announce-only", "groups create", "groups description",
	"groups invite link revoke", "groups join", "groups leave", "groups locked",
	"groups participants add", "groups participants demote", "groups participants promote",
	"groups participants remove", "groups rename", "groups requests approve",
	"groups requests reject", "groups topic", "messages delete", "messages edit",
	"messages forward", "messages revoke", "poll vote", "presence", "profile remove-picture",
	"profile set-about", "profile set-name", "profile set-picture", "send",
}

// readOnlyShort replaces the help line of groups whose upstream text promises
// more than what is left of them.
var readOnlyShort = map[string]string{
	"channels":            "Read WhatsApp channels",
	"groups":              "Read WhatsApp groups",
	"groups invite":       "Read group invite links",
	"groups invite link":  "Read group invite links",
	"groups participants": "List group participants",
	"groups requests":     "List group join requests",
	"poll":                "Inspect a poll",
	"profile":             "Read WhatsApp profiles",
}

// readOnlyDelegateKinds are the requests a running `sync --follow` still
// answers for other wacli processes. Both only read.
var readOnlyDelegateKinds = map[string]bool{
	mediaDownloadKind: true,
	contactsCheckKind: true,
}

func applyReadOnlyBuild(root *cobra.Command) {
	keep := map[string]bool{}
	for _, path := range readOnlyCommands {
		keep[path] = true
	}
	pruneCommands(root, "", keep)
	for path := range keep {
		panic(fmt.Sprintf("read-only build: command %q is gone; update readOnlyCommands", path))
	}
	for _, path := range removedCommands {
		at := strings.LastIndex(path, " ")
		parent := mustFindCommand(root, path[:max(at, 0)])
		if !hasCommand(parent, path[at+1:]) {
			parent.AddCommand(stubCommand(path))
		}
	}
	for path, short := range readOnlyShort {
		mustFindCommand(root, path).Short = short
	}
}

// pruneCommands cuts the commands below parent down to keep, and deletes from
// keep each path it finds. A command that is not listed is replaced by a stub;
// one that still has listed subcommands stays as a group that only prints help.
func pruneCommands(parent *cobra.Command, prefix string, keep map[string]bool) {
	for _, cmd := range parent.Commands() {
		path := strings.TrimSpace(prefix + " " + cmd.Name())
		pruneCommands(cmd, path, keep)
		switch {
		case keep[path]:
			delete(keep, path)
		case cmd.HasAvailableSubCommands():
			cmd.Run, cmd.RunE = nil, nil
		default:
			parent.RemoveCommand(cmd)
			parent.AddCommand(stubCommand(path))
		}
	}
}

// stubCommand is a hidden stub that takes any arguments and only says
// the command is not in this build.
func stubCommand(path string) *cobra.Command {
	return &cobra.Command{
		Use:                path[strings.LastIndex(path, " ")+1:],
		Short:              "Not available: this build cannot send to or change WhatsApp",
		Hidden:             true,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("%w: %q is not available", errReadOnlyBuild, "wacli "+path)
		},
	}
}

func hasCommand(parent *cobra.Command, name string) bool {
	return slices.ContainsFunc(parent.Commands(), func(sub *cobra.Command) bool { return sub.Name() == name })
}

func mustFindCommand(root *cobra.Command, path string) *cobra.Command {
	cmd := root
	for _, name := range strings.Fields(path) {
		i := slices.IndexFunc(cmd.Commands(), func(sub *cobra.Command) bool { return sub.Name() == name })
		if i < 0 {
			panic(fmt.Sprintf("read-only build: command %q is gone; update readonly.go", path))
		}
		cmd = cmd.Commands()[i]
	}
	return cmd
}

// newLocalLogoutCmd is `auth logout`: it forgets the paired device in the
// local session store and never contacts WhatsApp, where upstream's tells
// WhatsApp to remove the device. It is what lets a store pair again after
// WhatsApp revoked its session.
func newLocalLogoutCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget this device locally (does not contact WhatsApp)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()
			storeDir, err := resolveStoreDir(flags)
			if err != nil {
				return err
			}
			lk, err := lock.AcquireWithTimeout(ctx, storeDir, flags.lockWait)
			if err != nil {
				return err
			}
			defer lk.Release()
			forgot, err := wa.ForgetDevice(ctx, filepath.Join(storeDir, "session.db"))
			if err != nil {
				return err
			}
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"logged_out": forgot, "removed_on_whatsapp": false})
			}
			if !forgot {
				fmt.Fprintln(os.Stdout, "No paired device in this store.")
				return nil
			}
			fmt.Fprintln(os.Stdout, "Local session cleared. Run `wacli auth` to pair again.")
			fmt.Fprintln(os.Stdout, "WhatsApp was not contacted: if this device is still listed on your phone under Linked devices, remove it there.")
			return nil
		},
	}
}
