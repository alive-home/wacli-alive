package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestTaskRunsOnBatteryAndQuotesArgumentsWithSpaces(t *testing.T) {
	got := taskXML(Job{
		Label: "wacli-push-test", Program: `C:\Program Files\wacli\wacli.exe`,
		Args: []string{"--store", `C:\Users\Ada & Bo\.alive-wapp`, "push", "--installed"}, Every: 15 * time.Minute,
	}, `LAPTOP\ada`)
	for _, want := range []string{
		`<Command>C:\Program Files\wacli\wacli.exe</Command>`,
		`<Arguments>--store &#34;C:\Users\Ada &amp; Bo\.alive-wapp&#34; push --installed</Arguments>`,
		"<Interval>PT15M</Interval>",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
		"<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		`<UserId>LAPTOP\ada</UserId>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("task misses %q:\n%s", want, got)
		}
	}
	file := utf16File("<a/>")
	if len(file) != 10 || file[0] != 0xFF || file[1] != 0xFE || file[2] != '<' || file[3] != 0 {
		t.Fatalf("task file bytes = %v", file)
	}
}
