package outbound

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoveryAndGenerationRespectDirectOrServerProxy(t *testing.T) {
	var direct, proxyCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { direct.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(200) }))
	defer proxy.Close()
	u, _ := url.Parse(target.URL)
	for _, policy := range []string{"direct", "server"} {
		client, e := CredentialClient(context.Background(), target.URL, proxy.URL, policy, []string{u.Host}, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		r, e := client.Get(target.URL + "/models")
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		r.Body.Close()
		client.CloseIdleConnections()
	}
	if direct.Load() != 1 || proxyCalls.Load() != 1 {
		t.Fatal("provider proxy policy was ignored")
	}
	if _, e := CredentialClient(context.Background(), "http://127.0.0.1/v1", proxy.URL, "server", nil, time.Second); e == nil {
		t.Fatal("proxy bypassed destination validation")
	}
}
func TestPublicPageRedirectCannotReachPrivateHost(t *testing.T) {
	var calls atomic.Int32
	client := Client(nil, time.Second, 3)
	client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(&emptyReader{}), Request: r}, nil
	})
	if r, e := client.Get("https://example.com/source"); e == nil {
		r.Body.Close()
		t.Fatal("redirect entered private network")
	}
	if calls.Load() != 1 {
		t.Fatal("private redirect reached transport")
	}
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
