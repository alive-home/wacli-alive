// Package safehttp is the HTTP client for requests to a URL the user
// configured (the sync webhook): it only connects to public addresses.
package safehttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// NewClient returns a client that refuses every non-public destination, also
// after a redirect and whatever HTTP_PROXY says.
func NewClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&safeDialer{
			resolver: net.DefaultResolver,
			dialer:   &net.Dialer{Timeout: 5 * time.Second},
		}).DialContext,
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("stopped after 3 redirects")
			}
			if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return fmt.Errorf("invalid redirect URL")
			}
			return nil
		},
	}
}

type safeDialer struct {
	resolver ipResolver
	dialer   contextDialer
}

type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type contextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

func (d *safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	sawPublic := false
	for _, ip := range ips {
		if !publicIP(ip.IP) {
			continue
		}
		sawPublic = true
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if sawPublic && lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("URL resolves to a private or local address")
}

func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!addr.IsMulticast() &&
		!addr.IsUnspecified() &&
		!reservedIP(addr)
}

func reservedIP(addr netip.Addr) bool {
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}
