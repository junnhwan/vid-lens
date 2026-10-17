package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

type bilibiliInterruptedBody struct {
	sent bool
}

func (b *bilibiliInterruptedBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, io.ErrUnexpectedEOF
	}
	b.sent = true
	return copy(p, "part"), nil
}

func (*bilibiliInterruptedBody) Close() error { return nil }

func TestBilibiliDownloadRecoversUsingSuppliedBackup(t *testing.T) {
	for _, failure := range []string{"network", "forbidden", "server_error", "interrupted_body", "empty_body"} {
		t.Run(failure, func(t *testing.T) {
			var paths []string
			client := &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.bilibili.com/" || r.URL.Scheme != "https" {
					t.Fatal("CDN fallback changed anonymous request policy")
				}
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/backup" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("complete track")), ContentLength: 14, Header: http.Header{}}, nil
				}
				switch failure {
				case "network":
					return nil, errors.New("connection reset: https://cdn.example.test/primary?sig=do-not-echo")
				case "forbidden":
					return &http.Response{StatusCode: 403, Body: http.NoBody, Header: http.Header{}}, nil
				case "server_error":
					return &http.Response{StatusCode: 503, Body: http.NoBody, Header: http.Header{}}, nil
				case "interrupted_body":
					return &http.Response{StatusCode: 200, Body: &bilibiliInterruptedBody{}, ContentLength: 14, Header: http.Header{}}, nil
				default:
					return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
				}
			})}
			dir := t.TempDir()
			dst := filepath.Join(dir, "track")
			track := bilibiliTrack{BaseURL: "https://cdn.example.test/primary?sig=do-not-echo", BackupURL: []string{"http://cdn.example.test/backup"}}
			if err := (&bilibiliClient{http: client}).download(context.Background(), track, dst, 100); err != nil {
				t.Fatalf("supplied backup did not recover the complete track: %v", err)
			}
			data, err := os.ReadFile(dst)
			if err != nil || string(data) != "complete track" || !reflect.DeepEqual(paths, []string{"/primary", "/backup"}) {
				t.Fatalf("incomplete or concatenated media: %q, visits=%v, err=%v", data, paths, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 || entries[0].Name() != "track" {
				t.Fatal("CDN attempts left temporary files behind")
			}
		})
	}
}

func TestBilibiliDownloadDeduplicatesAndBoundsPublicCDNAlternates(t *testing.T) {
	var paths []string
	client := &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return &http.Response{StatusCode: 503, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	track := bilibiliTrack{BaseURL: "//cdn.example.test/primary?sig=do-not-echo", BackupURL: []string{
		"", "http://cdn.example.test/primary?sig=do-not-echo", "http://127.0.0.1/private", "https://localhost/private", "file:///private",
		"https://cdn.example.test/backup-one", "https://cdn.example.test/backup-one", "https://cdn.example.test/backup-two", "https://cdn.example.test/backup-three",
	}}
	dir := t.TempDir()
	err := (&bilibiliClient{http: client}).download(context.Background(), track, filepath.Join(dir, "track"), 100)
	if err == nil || strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("exhausted CDN attempts accepted or leaked a signed URL: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{"/primary", "/backup-one", "/backup-two"}) {
		t.Fatalf("unbounded, duplicate or nonpublic CDN attempts: %v", paths)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("failed download retained temporary files")
	}
}

func TestBilibiliDownloadDoesNotResetByteBudgetBetweenCDNs(t *testing.T) {
	var paths []string
	client := &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/primary" {
			return &http.Response{StatusCode: 200, Body: &bilibiliInterruptedBody{}, ContentLength: 8, Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("complete")), ContentLength: 8, Header: http.Header{}}, nil
	})}
	dir := t.TempDir()
	track := bilibiliTrack{BaseURL: "https://cdn.example.test/primary", BackupURL: []string{"https://cdn.example.test/backup", "https://cdn.example.test/unused"}}
	err := (&bilibiliClient{http: client}).download(context.Background(), track, filepath.Join(dir, "track"), 10)
	var p *domain.Problem
	if !errors.As(err, &p) || p.Code != "INPUT_LIMIT_EXCEEDED" || !reflect.DeepEqual(paths, []string{"/primary", "/backup"}) {
		t.Fatalf("CDN retry reset or retried the exhausted byte budget: paths=%v err=%v", paths, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("quota rejection retained partial media")
	}
}

func TestBilibiliDownloadCancellationDoesNotTryAnotherCDN(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, r.Context().Err()
	})}
	dir := t.TempDir()
	track := bilibiliTrack{BaseURL: "https://cdn.example.test/primary", BackupURL: []string{"https://cdn.example.test/backup"}}
	err := (&bilibiliClient{http: client}).download(ctx, track, filepath.Join(dir, "track"), 100)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation retried another CDN: calls=%d err=%v", calls, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("cancelled download retained temporary files")
	}
}

func TestBilibiliDownloadPreservesExistingDestination(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "track")
	if err := os.WriteFile(dst, []byte("existing media"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: bilibiliRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("replacement")), Header: http.Header{}}, nil
	})}
	err := (&bilibiliClient{http: client}).download(context.Background(), bilibiliTrack{BaseURL: "https://cdn.example.test/primary"}, dst, 100)
	data, readErr := os.ReadFile(dst)
	if err == nil || readErr != nil || string(data) != "existing media" || calls != 0 {
		t.Fatalf("preexisting destination was touched: data=%q calls=%d err=%v readErr=%v", data, calls, err, readErr)
	}
}
