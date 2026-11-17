package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// statusReceiver answers every request with one body and keeps what it was sent.
func statusReceiver(t *testing.T, code int, body string) (*fixture, *[]map[string]any) {
	t.Helper()
	f := newFixture(t)
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Wacli-Signature") != signature([]byte(testKey), raw) {
			t.Errorf("the status request is not signed with the key")
		}
		var sent map[string]any
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		got = append(got, sent)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	f.opts.URL = srv.URL
	return f, &got
}

func TestAskStatusReadsTheFourCounters(t *testing.T) {
	f, got := statusReceiver(t, http.StatusOK, `{"last_read_at":"2026-11-17T11:31:00.123456+01:00","reads_today":3,"messages":1204,"calls":17,"later":"ignored"}`)
	f.message("a", "one")
	status, err := AskStatus(context.Background(), f.db, f.opts)
	if err != nil {
		t.Fatalf("AskStatus: %v", err)
	}
	want := time.Date(2026, 11, 17, 10, 31, 0, 0, time.UTC)
	if status.LastReadAt == nil || !status.LastReadAt.Equal(want) || status.ReadsToday != 3 || status.Messages != 1204 || status.Calls != 17 {
		t.Fatalf("status = %+v", status)
	}
	// The Mac app reads times without a fraction of a second.
	if shown, _ := json.Marshal(status); !strings.Contains(string(shown), `"last_read_at":"2026-11-17T10:31:00Z"`) {
		t.Fatalf("the status is printed as %s", shown)
	}
	// One request that asks, says more follows and carries no content.
	if len(*got) != 1 {
		t.Fatalf("requests = %d, want 1", len(*got))
	}
	sent := (*got)[0]
	if sent["status"] != true || sent["more"] != true || len(sent["messages"].([]any)) != 0 {
		t.Fatalf("the status request = %v", sent)
	}
	// Asking moves no position: the first run still copies everything.
	if _, reqs := f.run(); len(reqs) != 0 {
		t.Fatalf("the fixture's own receiver got %d requests", len(reqs))
	}
}

func TestAskStatusTakesANeverReadKey(t *testing.T) {
	f, _ := statusReceiver(t, http.StatusOK, `{"last_read_at":null,"reads_today":0,"messages":0,"calls":0}`)
	status, err := AskStatus(context.Background(), f.db, f.opts)
	if err != nil || status.LastReadAt != nil {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}

func TestAskStatusRefusesWhatIsNotAStatus(t *testing.T) {
	long := `{"last_read_at":null,"reads_today":0,"messages":0,"calls":0,"pad":"` + strings.Repeat("x", maxStatusBytes) + `"}`
	for name, tc := range map[string]struct {
		code     int
		body     string
		noStatus bool
		says     string
	}{
		"an older receiver":   {code: http.StatusNoContent, noStatus: true},
		"an empty 200":        {code: http.StatusOK, noStatus: true},
		"another 200":         {code: http.StatusOK, body: `{"ok":true}`, noStatus: true},
		"text":                {code: http.StatusOK, body: `<html>hello</html>`, says: "cannot be read"},
		"a count that is not": {code: http.StatusOK, body: `{"last_read_at":null,"reads_today":"many","messages":0,"calls":0}`, says: "cannot be read"},
		"a negative count":    {code: http.StatusOK, body: `{"last_read_at":null,"reads_today":0,"messages":-1,"calls":0}`, says: "negative"},
		"too long":            {code: http.StatusOK, body: long, says: "longer than"},
		"a refusal":           {code: http.StatusUnauthorized, says: "answered 401"},
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := statusReceiver(t, tc.code, tc.body)
			_, err := AskStatus(context.Background(), f.db, f.opts)
			switch {
			case err == nil:
				t.Fatal("no error")
			case tc.noStatus && !errors.Is(err, ErrNoStatus):
				t.Fatalf("err = %v, want ErrNoStatus", err)
			case !tc.noStatus && !strings.Contains(err.Error(), tc.says):
				t.Fatalf("err = %v, want one that says %q", err, tc.says)
			case strings.Contains(err.Error(), "hello") || strings.Contains(err.Error(), "many"):
				t.Fatalf("the error repeats what the receiver answered: %v", err)
			}
		})
	}
}
