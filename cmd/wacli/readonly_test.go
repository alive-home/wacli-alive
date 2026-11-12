package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runnableCommands lists every command of the tree that can run, by path.
func runnableCommands(cmd *cobra.Command, prefix string) []string {
	var paths []string
	for _, sub := range cmd.Commands() {
		path := strings.TrimSpace(prefix + " " + sub.Name())
		if sub.Runnable() && !sub.Hidden {
			paths = append(paths, path)
		}
		paths = append(paths, runnableCommands(sub, path)...)
	}
	return paths
}

// The command tree is exactly readOnlyCommands: nothing upstream adds runs
// until it is listed there.
func TestReadOnlyBuildRunsOnlyListedCommands(t *testing.T) {
	got := runnableCommands(newRootCmd(&rootFlags{}), "")
	want := slices.Clone(readOnlyCommands)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("runnable commands = %q, want %q", got, want)
	}
}

func TestReadOnlyBuildRefusesSendingCommands(t *testing.T) {
	for _, args := range [][]string{
		{"send", "text", "--to", "15550001111", "--message", "hi"},
		{"send", "file", "--to", "15550001111", "--file", "x.png"},
		{"poll", "vote", "--to", "15550001111", "--id", "A", "--option", "1"},
		{"presence", "typing", "--to", "15550001111"},
		{"presence", "subscribe", "--to", "15550001111"},
		{"chats", "mark-read", "--chat", "15550001111"},
		{"chats", "archive", "--chat", "15550001111"},
		{"contacts", "save", "--jid", "15550001111", "--name", "Ada", "--account-wide"},
		{"messages", "edit", "--chat", "15550001111", "--id", "A", "--message", "x"},
		{"messages", "revoke", "--chat", "15550001111", "--id", "A"},
		{"messages", "forward", "--chat", "15550001111", "--id", "A", "--to", "15550002222"},
		{"groups", "create", "--name", "x", "--user", "15550001111"},
		{"groups", "leave", "--jid", "123@g.us"},
		{"groups", "invite", "link", "revoke", "--jid", "123@g.us"},
		{"channels", "join", "--invite", "x"},
		{"profile", "set-name", "x"},
	} {
		err := execute(append([]string{"--store", t.TempDir()}, args...))
		if !errors.Is(err, errReadOnlyBuild) {
			t.Errorf("%q: err = %v, want errReadOnlyBuild", args, err)
		}
	}
}

// A running `sync --follow` answers other wacli processes over its socket.
// Only the read-only requests get as far as doing any work: a delegated text
// would otherwise fetch its link preview before the WAClient guard refuses
// the send.
func TestDelegateRefusesEverythingButReads(t *testing.T) {
	for _, kind := range []string{
		"", "text", "file", "voice", "sticker", "react", "location", "poll", "poll_vote",
		"button_list_select", "presence", presenceSubscribeKind, "edit",
		markReadKind, markReadThroughKind, markReadReceiptsKind, chatStateKind, contactsSaveKind,
		"something-upstream-adds",
	} {
		_, err := executeDelegatedSend(context.Background(), nil, sendDelegateRequest{Version: sendDelegateVersion, Kind: kind})
		if !errors.Is(err, errReadOnlyBuild) {
			t.Errorf("kind %q: err = %v, want errReadOnlyBuild", kind, err)
		}
	}
	if len(readOnlyDelegateKinds) != 2 || !readOnlyDelegateKinds[mediaDownloadKind] || !readOnlyDelegateKinds[contactsCheckKind] {
		t.Fatalf("readOnlyDelegateKinds = %v, want only media download and contacts check", readOnlyDelegateKinds)
	}
}

// The documentation only shows commands this build has: every `wacli ...`
// line in a code block must name a command that runs.
func TestDocsOnlyShowAvailableCommands(t *testing.T) {
	files, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "../../README.md", "../../.agents/skills/wacli/SKILL.md")
	root := newRootCmd(&rootFlags{})
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		inCode := false
		for n, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCode = !inCode
				continue
			}
			words := strings.Fields(line)
			at := slices.Index(words, "wacli")
			if !inCode || at < 0 || strings.HasPrefix(words[0], "#") {
				continue
			}
			if problem := unavailableCommand(root, words[at+1:]); problem != "" {
				t.Errorf("%s:%d: %s: %s", filepath.Base(file), n+1, problem, strings.TrimSpace(line))
			}
		}
	}
}

// unavailableCommand follows a documented command line down the command tree
// and says what is wrong with it, or returns "".
func unavailableCommand(root *cobra.Command, words []string) string {
	cmd := root
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "-") {
			if flag := root.PersistentFlags().Lookup(strings.TrimLeft(word, "-")); flag != nil && flag.Value.Type() != "bool" {
				i++
			}
			continue
		}
		at := slices.IndexFunc(cmd.Commands(), func(sub *cobra.Command) bool { return sub.Name() == word })
		if at < 0 {
			placeholder := strings.HasPrefix(word, "[") || strings.HasPrefix(word, "<")
			if cmd == root && !placeholder && word != "help" && word != "completion" {
				return "unknown command " + word
			}
			break
		}
		cmd = cmd.Commands()[at]
		if cmd.Hidden {
			return "command not in this build"
		}
	}
	return ""
}

// `auth logout` only forgets the device locally. With no session there is
// nothing to connect with, so succeeding here shows it never needs WhatsApp.
func TestAuthLogoutIsLocalOnly(t *testing.T) {
	dir := t.TempDir()
	if err := execute([]string{"--store", dir, "--json", "auth", "logout"}); err != nil {
		t.Fatalf("auth logout on an unpaired store: %v", err)
	}
	if err := execute([]string{"--store", dir, "--read-only", "auth", "logout"}); err == nil {
		t.Fatal("auth logout under --read-only: err = nil, want rejection")
	}
}
