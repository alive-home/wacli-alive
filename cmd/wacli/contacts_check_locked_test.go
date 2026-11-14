package main

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/wacli/internal/lock"
)

func TestContactsCheckUsesLockedStoreDelegate(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	stop, err := startDelegateServerForStore(context.Background(), storeDir, func(context.Context, delegateRequest) (delegateResponse, error) {
		return delegateResponse{OK: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	stdout, stderr, err := runDelegateHelper(t, []string{
		"--store", storeDir, "--json", "--timeout", "750ms", "contacts", "check", "+15550000001",
	})
	if err != nil {
		t.Fatalf("contacts check: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestContactsCheckReadOnlyRejectsBeforeDelegation(t *testing.T) {
	skipDelegateSocketTestOnUnsupportedOS(t)
	for _, mode := range []string{"flag", "environment"} {
		t.Run(mode, func(t *testing.T) {
			storeDir := shortDelegateStoreDir(t)
			lk, err := lock.Acquire(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			defer lk.Release()
			called := make(chan struct{}, 1)
			stop, err := startDelegateServerForStore(context.Background(), storeDir, func(context.Context, delegateRequest) (delegateResponse, error) {
				called <- struct{}{}
				return delegateResponse{OK: true}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			args := []string{"--store", storeDir, "--json"}
			t.Setenv("WACLI_READONLY", "")
			if mode == "flag" {
				args = append(args, "--read-only")
			} else {
				t.Setenv("WACLI_READONLY", "1")
			}
			args = append(args, "contacts", "check", "+15550000001")
			stdout, stderr, err := runDelegateHelper(t, args)
			if err == nil || !strings.Contains(stdout+stderr, "read-only") {
				t.Fatalf("want read-only rejection, got %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			select {
			case <-called:
				t.Fatal("read-only check reached the delegate")
			default:
			}
		})
	}
}
