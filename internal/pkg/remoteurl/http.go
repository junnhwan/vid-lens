package remoteurl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPOptions defines a server-controlled target policy. Proxying is excluded:
// an ordinary HTTP CONNECT proxy resolves its target independently, so it does
// not provide the same validated-address connection guarantee as this client.
type HTTPOptions struct {
	AllowedHosts []string
	Resolver     Resolver
	DialContext  func(context.Context, string, string) (net.Conn, error)
	CookieJar    http.CookieJar
	MaxBytes     int64
	MaxRedirects int
	Timeout      time.Duration
}

type HTTPClient struct {
	client       *http.Client
	allowedHosts []string
	maxBytes     int64
}

// NewHTTPClient connects directly to the public IP it just validated, retaining
// normal TLS verification against the original hostname. Environment proxy
// settings are deliberately ignored. Redirects pass the same target policy.
func NewHTTPClient(options HTTPOptions) (*HTTPClient, error) {
	if len(options.AllowedHosts) == 0 {
		return nil, fmt.Errorf("HTTP target allowlist is required")
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = systemResolver{}
	}
	dial := options.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	maxRedirects := options.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 5
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	allowed := append([]string(nil), options.AllowedHosts...)
	transport := &http.Transport{
		Proxy:                  nil,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || (port != "80" && port != "443") || !HostAllowed(host, allowed) {
				return nil, fmt.Errorf("HTTP connection target rejected")
			}
			ips, err := resolver.LookupIP(ctx, NormalizeHost(host))
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("HTTP target DNS lookup failed")
			}
			for _, ip := range ips {
				if UnsafeIP(ip) {
					return nil, fmt.Errorf("HTTP target resolved to unsafe address")
				}
			}
			for _, ip := range ips {
				conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			return nil, fmt.Errorf("HTTP target connection failed")
		},
	}
	h := &HTTPClient{allowedHosts: allowed, maxBytes: maxBytes}
	h.client = &http.Client{Transport: transport, Jar: options.CookieJar, Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("HTTP redirect limit exceeded")
			}
			return h.validateTarget(req.URL)
		},
	}
	return h, nil
}

func (c *HTTPClient) validateTarget(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !HostAllowed(u.Hostname(), c.allowedHosts) {
		return fmt.Errorf("HTTP request target rejected")
	}
	port := u.Port()
	if port != "" && !((u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443")) {
		return fmt.Errorf("HTTP request port rejected")
	}
	if strings.ContainsAny(u.Host, "\r\n") {
		return fmt.Errorf("HTTP request host rejected")
	}
	return nil
}

// Get returns a bounded body and final redirect destination. Errors intentionally
// omit request URLs and provider response bodies, which may contain signed
// query values, cookies or sensitive provider information.
func (c *HTTPClient) Get(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || c.validateTarget(u) != nil {
		return nil, "", fmt.Errorf("HTTP request target rejected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP request creation failed")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 VidLens")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	response, err := c.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP request returned status %d", response.StatusCode)
	}
	if response.ContentLength > c.maxBytes {
		return nil, "", fmt.Errorf("HTTP response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("HTTP response read failed")
	}
	if int64(len(data)) > c.maxBytes {
		return nil, "", fmt.Errorf("HTTP response exceeds size limit")
	}
	return data, response.Request.URL.String(), nil
}

func (c *HTTPClient) CloseIdleConnections() { c.client.CloseIdleConnections() }
