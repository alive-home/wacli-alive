package schedule

import "testing"

func TestCommandLineQuotesPartsWithSpaces(t *testing.T) {
	got := commandLine(Job{Program: `C:\Program Files\wacli\wacli.exe`, Args: []string{"--store", `C:\Users\Ada Lovelace\.alive-wapp`, "push", "--installed"}})
	want := `"C:\Program Files\wacli\wacli.exe" --store "C:\Users\Ada Lovelace\.alive-wapp" push --installed`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
