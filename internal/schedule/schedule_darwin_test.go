package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestPlistEscapesArgumentsAndSetsTheInterval(t *testing.T) {
	got := string(plist(Job{Label: "wacli-push", Program: "/Applications/wacli.app/Contents/Helpers/wacli",
		Args: []string{"--store", "/Users/a&b/<store>", "push", "--installed"}, Every: 15 * time.Minute}))
	for _, want := range []string{
		"<key>Label</key><string>wacli-push</string>",
		"<string>/Users/a&amp;b/&lt;store&gt;</string><string>push</string><string>--installed</string>",
		"<key>StartInterval</key><integer>900</integer>",
		"<key>RunAtLoad</key><true/>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plist misses %q:\n%s", want, got)
		}
	}
}

func TestInstallRefusesAnAppThatRunsFromItsDownload(t *testing.T) {
	err := Install(Job{Label: "wacli-test-never-installed", Program: "/private/var/folders/x/T/AppTranslocation/1234/d/wacli.app/Contents/Helpers/wacli"})
	if err == nil || !strings.Contains(err.Error(), "move it to Applications") {
		t.Fatalf("err = %v", err)
	}
}
