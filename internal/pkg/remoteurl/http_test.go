package remoteurl

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type hostResolver map[string][]net.IP

func (r hostResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) { return r[host], nil }

func fixtureHTTPClient(t *testing.T, handler http.HandlerFunc, resolver Resolver, maxBytes int64, maxRedirects int) (*HTTPClient, *[]string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	addresses := &[]string{}
	var mu sync.Mutex
	client, err := NewHTTPClient(HTTPOptions{AllowedHosts: []string{"example.com"}, Resolver: resolver, MaxBytes: maxBytes, MaxRedirects: maxRedirects, Timeout: time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			*addresses = append(*addresses, address)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client, addresses
}

func TestHTTPPinsValidatedIPAndChecksEveryRedirect(t *testing.T) {
	client, addresses := fixtureHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://private.example.com/secret?signature=private", 302)
			return
		}
		w.Write([]byte("ok"))
	}, hostResolver{"example.com": {net.ParseIP("8.8.8.8")}, "private.example.com": {net.ParseIP("127.0.0.1")}}, 100, 3)
	body, final, err := client.Get(context.Background(), "http://example.com/ok")
	if err != nil || string(body) != "ok" || final != "http://example.com/ok" || len(*addresses) != 1 || (*addresses)[0] != "8.8.8.8:80" {
		t.Fatalf("unpinned connection: %s %s %v %v", body, final, err, *addresses)
	}
	_, _, err = client.Get(context.Background(), "http://example.com/redirect")
	if err == nil || strings.Contains(err.Error(), "signature") || len(*addresses) != 1 {
		t.Fatalf("unsafe redirect dialed or leaked: %v %v", err, *addresses)
	}
}

func TestHTTPRejectsUnsafeMixedDNSAndDisallowedTargets(t *testing.T) {
	client, addresses := fixtureHTTPClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }, hostResolver{"example.com": {net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1")}}, 100, 3)
	for _, raw := range []string{"http://example.com/", "http://evil.test/", "http://example.com:8080/", "https://user:secret@example.com/", "file:///private/path"} {
		if _, _, err := client.Get(context.Background(), raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if len(*addresses) != 0 {
		t.Fatal("dialed rejected DNS or target")
	}
}

func TestHTTPResponseAndRedirectBudgets(t *testing.T) {
	client, _ := fixtureHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, "http://example.com/loop", 302)
			return
		}
		w.Write([]byte("too many bytes"))
	}, hostResolver{"example.com": {net.ParseIP("8.8.8.8")}}, 5, 2)
	for _, path := range []string{"/big", "/loop"} {
		if _, _, err := client.Get(context.Background(), "http://example.com"+path); err == nil {
			t.Fatalf("budget not enforced %s", path)
		}
	}
}
