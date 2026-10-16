package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// CredentialClient shares one verified destination policy between model
// discovery and generation. A server-configured proxy cannot resolve the target
// to an unchecked internal address; its CONNECT destination is a pinned IP.
func CredentialClient(ctx context.Context, endpoint, proxy, policy string, allowed []string, timeout time.Duration) (*http.Client, error) {
	if policy == "direct" {
		proxy = ""
	}
	target, err := Validate(endpoint, allowed, true)
	if err != nil {
		return nil, err
	}
	tr := Transport(allowed)
	tr.ResponseHeaderTimeout = min(timeout, 90*time.Second)
	var transport http.RoundTripper = tr
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("invalid server proxy")
		}
		tr.Proxy = http.ProxyURL(u)
		tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
		if !slices.Contains(allowed, target.Host) {
			resolve, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			ips, err := PublicAddresses(resolve, target.Hostname())
			if err != nil {
				return nil, err
			}
			port := target.Port()
			if port == "" {
				port = "443"
			}
			tr.TLSClientConfig = &tls.Config{ServerName: target.Hostname(), MinVersion: tls.VersionTLS12}
			transport = pinnedProxy{base: tr, host: target.Host, pinned: net.JoinHostPort(ips[0].String(), port)}
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type pinnedProxy struct {
	base         http.RoundTripper
	host, pinned string
}

func (t pinnedProxy) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Host = t.pinned
	copy.URL = &u
	copy.Host = t.host
	return t.base.RoundTrip(copy)
}
