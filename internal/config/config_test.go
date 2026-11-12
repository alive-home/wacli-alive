package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/openclaw/wacli/internal/fsutil"
)

func TestDefaultStoreDir(t *testing.T) {
	t.Run("env var overrides default", func(t *testing.T) {
		t.Setenv(EnvStoreDir, "/custom/store/path")
		got := DefaultStoreDir()
		if got != "/custom/store/path" {
			t.Errorf("DefaultStoreDir() = %q, want %q", got, "/custom/store/path")
		}
	})

	t.Run("falls back to platform default when env unset", func(t *testing.T) {
		t.Setenv(EnvStoreDir, "")
		t.Setenv("XDG_STATE_HOME", "")
		got := DefaultStoreDir()
		home, _ := os.UserHomeDir()
		want := defaultStoreDirFor(runtime.GOOS, home, "")
		if got != want {
			t.Errorf("DefaultStoreDir() = %q, want %q", got, want)
		}
	})

	t.Run("env var constant is WACLI_STORE_DIR", func(t *testing.T) {
		if EnvStoreDir != "WACLI_STORE_DIR" {
			t.Errorf("EnvStoreDir = %q, want %q", EnvStoreDir, "WACLI_STORE_DIR")
		}
	})
}

func TestDefaultStoreDirFor(t *testing.T) {
	for _, tc := range []struct {
		name, goos, home, xdg, want string
	}{
		{"uses XDG_STATE_HOME on linux", "linux", "/home/alice", "/state", filepath.Join("/state", "alive-wapp")},
		{"uses XDG default on linux", "linux", "/home/alice", "", filepath.Join("/home/alice", ".local", "state", "alive-wapp")},
		{"ignores relative XDG_STATE_HOME on linux", "linux", "/home/alice", "state", filepath.Join("/home/alice", ".local", "state", "alive-wapp")},
		{"uses a home dotdir outside linux", "darwin", "/Users/alice", "/state", filepath.Join("/Users/alice", ".alive-wapp")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultStoreDirFor(tc.goos, tc.home, tc.xdg); got != tc.want {
				t.Fatalf("defaultStoreDirFor = %q, want %q", got, tc.want)
			}
		})
	}
}

// The default store must not sit where upstream wacli keeps its own: that
// path is the one a scan of well-known locations looks at.
func TestDefaultStoreDirIsNotUpstreams(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, xdg := range []string{"", "/state"} {
			if got := defaultStoreDirFor(goos, "/home/alice", xdg); strings.Contains(strings.ToLower(got), "wacli") {
				t.Fatalf("default store on %s is %q, which names wacli", goos, got)
			}
		}
	}
}

func TestAccountsConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &AccountsConfig{
		DefaultAccount: "personal",
		Accounts: map[string]AccountEntry{
			"personal": {Store: "accounts/personal"},
			"work":     {Store: "/tmp/wacli-work", Label: "Work"},
		},
	}

	if err := SaveAccountsConfig(path, cfg); err != nil {
		t.Fatalf("SaveAccountsConfig: %v", err)
	}
	loaded, err := LoadAccountsConfig(path)
	if err != nil {
		t.Fatalf("LoadAccountsConfig: %v", err)
	}
	if loaded.DefaultAccount != "personal" {
		t.Fatalf("DefaultAccount = %q, want personal", loaded.DefaultAccount)
	}
	store, account, err := ResolveAccountStore(path, "personal")
	if err != nil {
		t.Fatalf("ResolveAccountStore: %v", err)
	}
	wantStore := filepath.Join(filepath.Dir(path), "accounts", "personal")
	if store != wantStore || account.StoreDir != wantStore {
		t.Fatalf("store = %q/%q, want %q", store, account.StoreDir, wantStore)
	}
	if !account.Default {
		t.Fatal("account.Default = false, want true")
	}
}

func TestAccountsConfigKnownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := fsutil.WritePrivateFile(path, []byte("unknown: true\n")); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAccountsConfig(path)
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("LoadAccountsConfig error = %v, want unknown field error", err)
	}
}

func TestSaveAccountsConfigRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *AccountsConfig
		want string
	}{
		{
			name: "bad account name",
			cfg: &AccountsConfig{Accounts: map[string]AccountEntry{
				"bad/name": {Store: "accounts/bad"},
			}},
			want: "invalid account name",
		},
		{
			name: "missing store",
			cfg: &AccountsConfig{Accounts: map[string]AccountEntry{
				"bad": {Store: " "},
			}},
			want: "store is required",
		},
		{
			name: "uri metacharacter",
			cfg: &AccountsConfig{Accounts: map[string]AccountEntry{
				"bad": {Store: "accounts/bad?mode=memory"},
			}},
			want: "must not contain '?' or '#'",
		},
		{
			name: "undefined default",
			cfg: &AccountsConfig{
				DefaultAccount: "missing",
				Accounts:       map[string]AccountEntry{"work": {Store: "accounts/work"}},
			},
			want: "default_account \"missing\" is not defined",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			err := SaveAccountsConfig(path, tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SaveAccountsConfig error = %v, want %q", err, tc.want)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("config file exists after failed save: %v", statErr)
			}
		})
	}
}

func TestValidateAccountName(t *testing.T) {
	valid := []string{"personal", "work-2", "client.foo", "a_b"}
	for _, name := range valid {
		if err := ValidateAccountName(name); err != nil {
			t.Fatalf("ValidateAccountName(%q): %v", name, err)
		}
	}

	invalid := []string{"", " work", "work ", ".hidden", "-work", "bad/name", "bad?name", "bad name"}
	for _, name := range invalid {
		if err := ValidateAccountName(name); err == nil {
			t.Fatalf("ValidateAccountName(%q) succeeded, want error", name)
		}
	}
}
