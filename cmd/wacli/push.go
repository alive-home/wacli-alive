package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	appPkg "github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/push"
	"github.com/openclaw/wacli/internal/store"
	"github.com/spf13/cobra"
)

type pushFlags struct {
	url          string
	keyFile      string
	allowPrivate bool
	dryRun       bool
	sync         bool
	every        time.Duration
}

func newPushCmd(flags *rootFlags) *cobra.Command {
	var p pushFlags
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Post stored messages and calls to a URL you choose",
		Long: "push posts what the local store holds to a URL you choose: every message once, then each\n" +
			"change, and the call events. It only posts. It keeps its place per URL, so a run sends\n" +
			"what that URL has not had yet.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if p.url == "" {
				return fmt.Errorf("--url is required")
			}
			if p.every != 0 && p.every < time.Minute {
				return fmt.Errorf("--every must be at least 1m, got %s", p.every)
			}
			if p.dryRun && (p.every != 0 || p.sync) {
				return fmt.Errorf("--dry-run only counts what is stored; it cannot be combined with --sync or --every")
			}
			if p.sync {
				if err := flags.requireWritable(); err != nil {
					return err
				}
			}
			var key []byte
			if p.keyFile != "" {
				data, err := readRegularFileLimited(p.keyFile, 4096)
				if err != nil {
					return fmt.Errorf("read --key-file: %w", err)
				}
				if key = bytes.TrimSpace(data); len(key) == 0 {
					return fmt.Errorf("--key-file is empty")
				}
			}
			ctx, stop := signalContextWithEvents(out.NewEventWriter(os.Stderr, flags.events))
			defer stop()
			for {
				res, err := pushRound(ctx, flags, p, key)
				if p.every == 0 || ctx.Err() != nil {
					if err != nil {
						return err
					}
					return writePushResult(flags, p, res)
				}
				// A round that fails keeps its place; the next one sends it again.
				if err != nil {
					fmt.Fprintf(os.Stderr, "push failed: %v; next try in %s\n", err, p.every)
				} else if err := writePushResult(flags, p, res); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(p.every):
				}
			}
		},
	}
	cmd.Flags().StringVar(&p.url, "url", "", "URL to POST to (https)")
	cmd.Flags().StringVar(&p.keyFile, "key-file", "", "file holding the key that signs each request (X-Wacli-Signature)")
	cmd.Flags().BoolVar(&p.allowPrivate, "allow-private", false, "allow http and URLs that resolve to localhost or private networks")
	cmd.Flags().BoolVar(&p.dryRun, "dry-run", false, "count what would be posted; post nothing and keep the position")
	cmd.Flags().BoolVar(&p.sync, "sync", false, "fetch what is new from WhatsApp first (as sync --once)")
	cmd.Flags().DurationVar(&p.every, "every", 0, "keep running and repeat after this long, e.g. 15m")
	return cmd
}

func pushRound(ctx context.Context, flags *rootFlags, p pushFlags, key []byte) (push.Result, error) {
	var syncErr error
	if p.sync {
		syncErr = syncOnceForPush(ctx, flags)
	}
	a, lk, err := newApp(ctx, flags, false, false)
	if err != nil {
		return push.Result{}, err
	}
	defer closeApp(a, lk)
	linked, _, err := readOnlyAuthStatus(a.StoreDir())
	if err != nil {
		return push.Result{}, err
	}
	res, err := push.Run(ctx, a.DB(), push.Options{
		URL: p.url, Key: key, AllowPrivate: p.allowPrivate, DryRun: p.dryRun,
		StoreDir: a.StoreDir(), Version: a.Version(), Linked: linked,
		ResolveNames: func(msgs []store.Message) []store.Message { return resolveMessageSenderNames(ctx, a, msgs) },
	})
	if err != nil {
		return res, err
	}
	if syncErr != nil {
		return res, fmt.Errorf("posted what was stored, but the sync before it failed: %w", syncErr)
	}
	return res, nil
}

// syncOnceForPush closes the WhatsApp connection before anything is posted.
func syncOnceForPush(ctx context.Context, flags *rootFlags) error {
	a, lk, err := newApp(ctx, flags, true, false)
	if err != nil {
		return err
	}
	defer closeApp(a, lk)
	if err := a.EnsureAuthed(ctx); err != nil {
		return err
	}
	_, err = a.Sync(ctx, appPkg.SyncOptions{Mode: appPkg.SyncModeOnce, IdleExit: 30 * time.Second, MaxReconnect: 5 * time.Minute})
	return err
}

func writePushResult(flags *rootFlags, p pushFlags, res push.Result) error {
	if flags.asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{"posted": !p.dryRun, "result": res})
	}
	verb := "Posted"
	if p.dryRun {
		verb = "Would post"
	}
	_, err := fmt.Fprintf(os.Stdout, "%s %d messages, %d removed, %d calls (requests: %d).\n", verb, res.Messages, res.Removed, res.Calls, res.Requests)
	return err
}
