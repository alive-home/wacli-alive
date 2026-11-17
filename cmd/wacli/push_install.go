package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/fsutil"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/push"
	"github.com/openclaw/wacli/internal/schedule"
	"github.com/spf13/cobra"
)

const (
	pushJobEvery = 15 * time.Minute
	// Both live in the store directory, owner-only like the rest of it.
	pushSettingsFile = "scheduled-push.json"
	pushLastRunFile  = "scheduled-push-last.json"
	// The key `push install --stdin` was handed.
	pushKeyFile = "scheduled-push.key"
)

// pushSettings is what `push install` saved for the scheduled runs. The URL
// stays in this file: it can hold a token, and a job's arguments are readable
// by every program on the computer.
type pushSettings struct {
	URL          string `json:"url"`
	KeyFile      string `json:"key_file,omitempty"`
	AllowPrivate bool   `json:"allow_private,omitempty"`
}

// pushLastRun is how the latest scheduled run ended.
type pushLastRun struct {
	At     time.Time    `json:"at"`
	OK     bool         `json:"ok"`
	Error  string       `json:"error,omitempty"`
	Result *push.Result `json:"result,omitempty"`
}

// pushScheduler is the system's scheduler. Tests put a recorder here.
var pushScheduler = struct {
	install   func(schedule.Job) error
	uninstall func(string) error
	installed func(string) (bool, error)
}{schedule.Install, schedule.Uninstall, schedule.Installed}

func newPushInstallCmd(flags *rootFlags) *cobra.Command {
	var s pushSettings
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Have this computer push every 15 minutes",
		Long: "install checks that the receiver takes the URL and key, saves them in the store directory,\n" +
			"and has the system start `push --installed` now and every 15 minutes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			var key []byte
			switch {
			case fromStdin && (s.URL != "" || s.KeyFile != ""):
				return fmt.Errorf("--stdin carries the URL and the key and cannot be combined with --url or --key-file")
			case fromStdin:
				var err error
				if s.URL, key, err = readPushInstallInput(cmd.InOrStdin()); err != nil {
					return err
				}
			case s.KeyFile != "":
				var err error
				if s.KeyFile, err = filepath.Abs(s.KeyFile); err != nil {
					return err
				}
				if key, err = readPushKey(s.KeyFile); err != nil {
					return err
				}
			}
			if s.URL == "" {
				return fmt.Errorf("--url is required")
			}
			storeDir, err := resolveStoreDir(flags)
			if err != nil {
				return err
			}
			label := pushJobLabel(storeDir)
			// A system without a scheduler says so before anything is checked or saved.
			if _, err := pushScheduler.installed(label); err != nil {
				return err
			}
			// The path as this program was started: a link that is moved to a
			// newer build takes the job along.
			program, err := os.Executable()
			if err != nil {
				return fmt.Errorf("find this program: %w", err)
			}
			ctx, stop := signalContextWithEvents(out.NewEventWriter(os.Stderr, flags.events))
			defer stop()
			if err := checkPushReceiver(ctx, flags, s, key); err != nil {
				return err
			}
			// From here a failure takes back what was written, an earlier
			// install included: a push is installed whole or not at all.
			err = savePushSettings(storeDir, s, key, fromStdin)
			if err == nil {
				err = pushScheduler.install(schedule.Job{
					Label: label, Program: program, Args: []string{"--store", storeDir, "push", "--installed"}, Every: pushJobEvery,
				})
			}
			if err != nil {
				_ = removeInstalledPush(storeDir, label)
				return fmt.Errorf("nothing is installed: %w", err)
			}
			return writePushStatus(flags, storeDir)
		},
	}
	cmd.Flags().StringVar(&s.URL, "url", "", "URL to POST to (https)")
	cmd.Flags().StringVar(&s.KeyFile, "key-file", "", "file holding the key that signs each request (X-Wacli-Signature)")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, `read the URL and the key from standard input as {"url": "...", "key": "..."}; the key is kept in the store directory`)
	cmd.Flags().BoolVar(&s.AllowPrivate, "allow-private", false, "allow http and URLs that resolve to a private network (a receiver on this computer needs no flag)")
	return cmd
}

// pushJobLabel names the job of one store, so two stores each keep their own
// schedule and `status` and `uninstall` speak for the store they are asked about.
func pushJobLabel(storeDir string) string {
	sum := sha256.Sum256([]byte(storeDir))
	return "wacli-push-" + hex.EncodeToString(sum[:4])
}

// readPushInstallInput reads what `--stdin` carries: a program that starts
// install hands over the URL and the key without putting them in arguments,
// which every program on the computer can read.
func readPushInstallInput(r io.Reader) (string, []byte, error) {
	var in struct {
		URL string `json:"url"`
		Key string `json:"key"`
	}
	data, err := io.ReadAll(io.LimitReader(r, 64<<10))
	if err != nil {
		return "", nil, fmt.Errorf("read --stdin: %w", err)
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return "", nil, fmt.Errorf(`read --stdin: want {"url": "...", "key": "..."}: %w`, err)
	}
	key := bytes.TrimSpace([]byte(in.Key))
	if len(key) > 4096 {
		return "", nil, fmt.Errorf("the key is longer than 4096 bytes")
	}
	return strings.TrimSpace(in.URL), key, nil
}

// savePushSettings writes what the scheduled rounds need. A key that came
// over standard input is kept beside the settings; a key kept by an earlier
// install goes when this one does not use it.
func savePushSettings(storeDir string, s pushSettings, key []byte, keepKey bool) error {
	stored := filepath.Join(storeDir, pushKeyFile)
	if keepKey && len(key) > 0 {
		s.KeyFile = stored
		if err := fsutil.WritePrivateFileAtomic(stored, append(key, '\n')); err != nil {
			return err
		}
	} else if err := os.Remove(stored); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return fsutil.WritePrivateFileAtomic(filepath.Join(storeDir, pushSettingsFile), data)
}

// removeInstalledPush takes the job out of the system's scheduler and removes
// the saved URL, the last result and a key install stored.
func removeInstalledPush(storeDir, label string) error {
	err := pushScheduler.uninstall(label)
	for _, name := range []string{pushSettingsFile, pushLastRunFile, pushKeyFile} {
		if removeErr := os.Remove(filepath.Join(storeDir, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
			err = removeErr
		}
	}
	return err
}

// checkPushReceiver posts the one request without content that proves the
// receiver takes this URL and key.
func checkPushReceiver(ctx context.Context, flags *rootFlags, s pushSettings, key []byte) error {
	a, lk, err := newApp(ctx, flags, false, false)
	if err != nil {
		return err
	}
	defer closeApp(a, lk)
	linked, _, err := readOnlyAuthStatus(a.StoreDir())
	if err != nil {
		return err
	}
	err = push.Check(ctx, a.DB(), push.Options{
		URL: s.URL, Key: key, AllowPrivate: s.AllowPrivate, StoreDir: a.StoreDir(), Version: a.Version(), Linked: linked,
	})
	if err != nil {
		return fmt.Errorf("nothing was installed: %w", err)
	}
	return nil
}

func newPushUninstallCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the scheduled push and forget its URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			storeDir, err := resolveStoreDir(flags)
			if err != nil {
				return err
			}
			if err := removeInstalledPush(storeDir, pushJobLabel(storeDir)); err != nil {
				return err
			}
			return writePushStatus(flags, storeDir)
		},
	}
}

func newPushStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether a scheduled push is installed and how its last run ended",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			storeDir, err := resolveStoreDir(flags)
			if err != nil {
				return err
			}
			return writePushStatus(flags, storeDir)
		},
	}
}

// runInstalledPush is one scheduled round: what is new from WhatsApp, then
// the push, with the URL and key `push install` saved. How it ended is kept
// for `push status`.
func runInstalledPush(flags *rootFlags) error {
	if err := flags.requireWritable(); err != nil {
		return err
	}
	storeDir, err := resolveStoreDir(flags)
	if err != nil {
		return err
	}
	s, err := readPushSettings(storeDir)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("no scheduled push is installed for this store; run `wacli push install`")
	}
	ctx, stop := signalContextWithEvents(out.NewEventWriter(os.Stderr, flags.events))
	defer stop()
	p := pushFlags{url: s.URL, keyFile: s.KeyFile, allowPrivate: s.AllowPrivate, sync: true}
	last := pushLastRun{At: time.Now().UTC().Truncate(time.Second)}
	res, err := func() (push.Result, error) {
		var key []byte
		if p.keyFile != "" {
			var err error
			if key, err = readPushKey(p.keyFile); err != nil {
				return push.Result{}, err
			}
		}
		return pushRound(ctx, flags, p, key)
	}()
	if last.OK = err == nil; last.OK {
		last.Result = &res
	} else {
		last.Error = err.Error()
	}
	if data, marshalErr := json.Marshal(last); marshalErr == nil {
		_ = fsutil.WritePrivateFileAtomic(filepath.Join(storeDir, pushLastRunFile), data)
	}
	if err != nil {
		return err
	}
	return writePushResult(flags, p, res)
}

func readPushKey(path string) ([]byte, error) {
	data, err := readRegularFileLimited(path, 4096)
	if err != nil {
		return nil, fmt.Errorf("read --key-file: %w", err)
	}
	key := bytes.TrimSpace(data)
	if len(key) == 0 {
		return nil, fmt.Errorf("--key-file is empty")
	}
	return key, nil
}

// readPushSettings returns nil when nothing is installed for this store.
func readPushSettings(storeDir string) (*pushSettings, error) {
	var s pushSettings
	if found, err := readJSONFile(filepath.Join(storeDir, pushSettingsFile), &s); err != nil || !found {
		return nil, err
	}
	return &s, nil
}

func readJSONFile(path string, into any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return true, nil
}

// writePushStatus prints what is installed. It names the receiver's host,
// never the URL.
func writePushStatus(flags *rootFlags, storeDir string) error {
	s, err := readPushSettings(storeDir)
	if err != nil {
		return err
	}
	scheduled, err := pushScheduler.installed(pushJobLabel(storeDir))
	if err != nil && !errors.Is(err, schedule.ErrUnsupported) {
		return err
	}
	var last *pushLastRun
	host := ""
	if s != nil {
		if u, err := url.Parse(s.URL); err == nil {
			host = u.Host
		}
		var run pushLastRun
		found, err := readJSONFile(filepath.Join(storeDir, pushLastRunFile), &run)
		if err != nil {
			return err
		}
		if found {
			last = &run
		}
	}
	if flags.asJSON {
		return out.WriteJSON(os.Stdout, map[string]any{
			"installed": s != nil, "scheduled": scheduled, "host": host,
			"every_seconds": int(pushJobEvery.Seconds()), "last_run": last,
		})
	}
	switch {
	case s == nil:
		fmt.Fprintln(os.Stdout, "No scheduled push is installed for this store.")
		return nil
	case !scheduled:
		fmt.Fprintf(os.Stdout, "A push to %s is saved, but the system no longer starts it. Run `wacli push install` again.\n", host)
	default:
		fmt.Fprintf(os.Stdout, "Pushes to %s every %d minutes.\n", host, int(pushJobEvery.Minutes()))
	}
	switch {
	case last == nil:
		fmt.Fprintln(os.Stdout, "No run has finished yet.")
	case last.OK:
		fmt.Fprintf(os.Stdout, "Last run %s: posted %d messages, %d removed, %d calls.\n",
			last.At.Local().Format("2006-01-02 15:04"), last.Result.Messages, last.Result.Removed, last.Result.Calls)
	default:
		fmt.Fprintf(os.Stdout, "Last run %s failed: %s\n", last.At.Local().Format("2006-01-02 15:04"), last.Error)
	}
	return nil
}
