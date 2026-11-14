package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRejectsLocalhost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)

	resp, err := NewClient().Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a request to localhost to be rejected")
	}
}

func TestClientIgnoresProxyForPrivateTargets(t *testing.T) {
	proxyCalled := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalled = true
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	resp, err := NewClient().Get("http://10.0.0.1/")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a private target to be rejected")
	}
	if proxyCalled {
		t.Fatal("client used HTTP_PROXY for a private target")
	}
}

func TestSafeDialerTriesRemainingPublicIPs(t *testing.T) {
	wantErr := errors.New("first failed")
	d := &safeDialer{
		resolver: fakeResolver{ips: []net.IPAddr{
			{IP: net.ParseIP("203.0.113.1")},
			{IP: net.ParseIP("93.184.216.34")},
		}},
		dialer: &fakeDialer{
			failByAddress: map[string]error{
				"93.184.216.34:443": wantErr,
			},
		},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "example.com:443")
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected first public dial error when only reserved address precedes it")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("DialContext error = %v, want %v", err, wantErr)
	}

	d.dialer = &fakeDialer{
		failByAddress: map[string]error{
			"93.184.216.34:443": wantErr,
		},
		successAddress: "142.250.72.14:443",
	}
	d.resolver = fakeResolver{ips: []net.IPAddr{
		{IP: net.ParseIP("93.184.216.34")},
		{IP: net.ParseIP("142.250.72.14")},
	}}
	conn, err = d.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
}

func TestPublicIPRejectsNonPublicRanges(t *testing.T) {
	for _, raw := range []string{
		"10.0.0.1",
		"100.64.0.1",
		"169.254.169.254",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"240.0.0.1",
		"::10.0.0.1",
		"64:ff9b::a00:1",
		"2002:a00:1::",
		"2001:db8::1",
		"fc00::1",
		"fec0::1",
	} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("publicIP(%s) = true, want false", raw)
		}
	}
	if !publicIP(net.ParseIP("93.184.216.34")) {
		t.Fatalf("expected public IPv4 to be allowed")
	}
	if !publicIP(net.ParseIP("2606:4700:4700::1111")) {
		t.Fatalf("expected public IPv6 to be allowed")
	}
}

type fakeResolver struct {
	ips []net.IPAddr
	err error
}

func (r fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.ips, r.err
}

type fakeDialer struct {
	failByAddress  map[string]error
	successAddress string
}

func (d *fakeDialer) DialContext(_ context.Context, _ string, address string) (net.Conn, error) {
	if err := d.failByAddress[address]; err != nil {
		return nil, err
	}
	if d.successAddress != "" && address != d.successAddress {
		return nil, errors.New("unexpected address")
	}
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}
