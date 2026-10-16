// Package outbound owns credential-free public-page networking. Address checks
// are repeated at every redirect and in the dialer, with DNS bound to the dial.
package outbound

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, raw := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}
func PublicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return nil, errors.New("public DNS lookup failed")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("DNS resolved to a blocked address")
		}
	}
	return ips, nil
}
func Validate(raw string, allowed []string, httpsOnly bool) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 4096 {
		return nil, errors.New("invalid public URL")
	}
	if slices.Contains(allowed, u.Host) && (u.Scheme == "http" || u.Scheme == "https") {
		return u, nil
	}
	if u.Scheme != "https" && (httpsOnly || u.Scheme != "http") {
		return nil, errors.New("public HTTPS URL required")
	}
	if u.Port() != "" && u.Port() != "443" && !(u.Scheme == "http" && u.Port() == "80") {
		return nil, errors.New("port is not allowed")
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.Contains(h, "%") || !strings.Contains(h, ".") {
		return nil, errors.New("private hostname is not allowed")
	}
	if ip, e := netip.ParseAddr(h); e == nil && !publicIP(ip) {
		return nil, errors.New("private address is not allowed")
	}
	return u, nil
}
func Transport(allowed []string) *http.Transport {
	return &http.Transport{Proxy: nil, DisableCompression: true, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConnsPerHost: 2, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		if slices.Contains(allowed, address) || slices.Contains(allowed, host) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("public DNS lookup failed")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("DNS resolved to a blocked address")
			}
		}
		var last error
		for _, ip := range ips {
			c, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}}
}
func Client(allowed []string, timeout time.Duration, redirects int) *http.Client {
	return &http.Client{Transport: Transport(allowed), Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > redirects {
			return errors.New("redirect limit exceeded")
		}
		if _, e := Validate(r.URL.String(), allowed, false); e != nil {
			return e
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		return nil
	}}
}
