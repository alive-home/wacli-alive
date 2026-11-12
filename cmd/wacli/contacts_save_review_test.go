package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestReview500ContactsSavePositionalArguments(t *testing.T) {
	cmd := newContactsCmd(&rootFlags{storeDir: t.TempDir(), readOnly: true})
	cmd.SetArgs([]string{"save", "--jid", "15550001111", "--name", "Ada", "extra"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil || strings.Contains(err.Error(), "read-only") {
		t.Fatalf("want positional rejection before RunE, got %v", err)
	}
}

func TestContactsSaveRequiresAccountWideOptInBeforeOpeningStore(t *testing.T) {
	for _, phonebook := range []bool{false, true} {
		cmd := newContactsCmd(&rootFlags{storeDir: t.TempDir()})
		args := []string{"save", "--jid", "15550001111", "--name", "Ada"}
		if phonebook {
			args = append(args, "--phonebook")
		}
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "requires --account-wide") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestDelegatedContactsSaveRequiresAccountWideOptIn(t *testing.T) {
	saver := &fakeContactSaver{}
	_, err := executeDelegatedContactsSave(context.Background(), saver, sendDelegateRequest{Kind: contactsSaveKind, To: "15550001111", Name: "Ada", Phonebook: true})
	if err == nil || !strings.Contains(err.Error(), "--account-wide") || len(saver.saved) != 0 {
		t.Fatalf("err=%v saved=%v", err, saver.saved)
	}
}
