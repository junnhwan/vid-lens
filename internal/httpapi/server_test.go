package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func apiTest(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	dsn := os.Getenv("VIDLENS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VIDLENS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	base, e := store.Open(ctx, dsn, strings.Repeat("secret", 8))
	if e != nil {
		t.Fatal(e)
	}
	schema := "test_" + domain.ID()
	if _, e = base.Pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, e := store.Open(ctx, u.String(), strings.Repeat("secret", 8))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	p := config.DefaultProfile()
	p.ModelBaseURL = "https://example.test/v1"
	p.ModelName = "test"
	if e = s.Bootstrap(ctx, p, "private-model-key", ""); e != nil {
		t.Fatal(e)
	}
	a := New(ctx, s, config.Config{Origin: "https://vidlens.test", SecretKey: strings.Repeat("secret", 8), AdminToken: strings.Repeat("admin", 8), SessionTTL: 30 * 24 * time.Hour, DataDir: t.TempDir()})
	server := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		cancel()
		server.Close()
		s.Pool.Close()
		_, _ = base.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		base.Pool.Close()
	})
	return server, a
}
func TestSessionCSRFAndPrivateResources(t *testing.T) {
	server, a := apiTest(t)
	do := func(method, path, body, csrf string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		req.Header.Set("Origin", "https://vidlens.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "network-action")
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	session := do("POST", "/api/v1/session", "{}", "", nil)
	if session.StatusCode != 201 {
		t.Fatal(session.StatusCode)
	}
	var sess map[string]any
	_ = json.NewDecoder(session.Body).Decode(&sess)
	cookie := session.Cookies()[0]
	csrf := sess["csrf_token"].(string)
	if res := do("POST", "/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk"}`, "wrong", cookie); res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	res := do("POST", "/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk"}`, csrf, cookie)
	if res.StatusCode != 201 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	location := res.Header.Get("Location")
	again := do("POST", "/api/v1/requests", `{"video_url":"https://youtube.com/watch?v=abcdefghijk"}`, csrf, cookie)
	if again.StatusCode != 200 || again.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("network replay failed")
	}
	other := do("POST", "/api/v1/session", "{}", "", nil).Cookies()[0]
	if r := do("GET", location, "", "", other); r.StatusCode != 404 {
		t.Fatal("cross-session request read")
	}
	canceled := do("POST", location+"/cancel", "{}", csrf, cookie)
	if canceled.StatusCode != 200 {
		t.Fatal(canceled.StatusCode)
	}
	events := do("GET", location+"/events", "", "", cookie)
	b, _ := io.ReadAll(events.Body)
	if !strings.Contains(string(b), "event: view.snapshot") || !strings.Contains(string(b), `"status":"canceled"`) {
		t.Fatalf("terminal SSE: %s", b)
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/admin/config", nil)
	req.Header.Set("Authorization", "Bearer "+a.Config.AdminToken)
	admin, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Body.Close()
	b, _ = io.ReadAll(admin.Body)
	if strings.Contains(string(b), "private-model-key") {
		t.Fatal("credential leaked")
	}
}

func TestOwnerCredentialUpdateReturnsStoredReferencesWithoutSecrets(t *testing.T) {
	server, a := apiTest(t)
	_, p, err := a.Store.CurrentConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	oldRef := p.ModelKeyRef
	p.ModelKeyRef = "caller-supplied-reference"
	req, err := http.NewRequest("PUT", server.URL+"/api/v1/admin/config", bytes.NewReader(domain.JSON(map[string]any{"profile": p, "model_api_key": "rotated-private-key"})))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.Config.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	wire, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("status=%d error=%v", res.StatusCode, err)
	}
	var saved struct {
		Profile config.Profile `json:"profile"`
	}
	if err = json.Unmarshal(wire, &saved); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "rotated-private-key") || saved.Profile.ModelKeyRef == p.ModelKeyRef || saved.Profile.ModelKeyRef == oldRef {
		t.Fatal("owner response exposed a key or did not return server-owned references")
	}
	old, err := a.Store.Credential(context.Background(), oldRef)
	if err != nil || old != "private-model-key" {
		t.Fatal("rotation broke an existing configuration revision")
	}
}

func TestSessionStoreOutageDoesNotClaimCookieExpiredOrReplaceIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := pgxpool.New(ctx, "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	a := New(ctx, &store.Store{Pool: pool}, config.Config{Origin: "https://vidlens.test"})
	for _, path := range []string{"/api/v1/requests/request-id", "/api/v1/session"} {
		method := "GET"
		if path == "/api/v1/session" {
			method = "POST"
		}
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Origin", "https://vidlens.test")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "existing-session-cookie"})
		res := httptest.NewRecorder()
		a.Handler().ServeHTTP(res, req)
		if res.Code != 503 || res.Header().Get("Set-Cookie") != "" || strings.Contains(res.Body.String(), "SESSION_REQUIRED") {
			t.Fatalf("%s lost session on a dependency error: %d %s", path, res.Code, res.Body.String())
		}
	}
}

func TestMissingImageUpdatesAvailabilityWithoutStartingWork(t *testing.T) {
	server, a := apiTest(t)
	ctx := context.Background()
	token := "owned-cookie"
	owner, err := a.Store.NewSession(ctx, hashToken(token), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := a.Store.CreateRequest(ctx, owner.ID, "image-loss", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := a.Store.Request(ctx, owner.ID, accepted.ID)
	c, err := a.Store.Claim(ctx, *r.ActiveRunID, 1, "fixture")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: "video", SHA256: "video", MIME: "video/mp4", Bytes: 1, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err = a.Store.AddAsset(ctx, *c, video); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "video", TextHash: "text", DurationMS: 1000, Source: domain.Source{SourceID: c.Run.SourceID}, Metadata: []byte(`{}`)}
	if _, err = a.Store.FinishSource(ctx, *c, snapshot, nil, video.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = a.Store.Request(ctx, owner.ID, r.ID)
	c, err = a.Store.Claim(ctx, *r.ActiveRunID, 1, "fixture")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	frame := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, SnapshotID: r.SnapshotID, RunID: c.Run.ID, Kind: "frame", StorageKey: "assets/image.jpg", SHA256: "frame", MIME: "image/jpeg", Bytes: 1, TimeMS: domain.Ptr(int64(100)), RequestedTimeMS: domain.Ptr(int64(100)), ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err = a.Store.AddAsset(ctx, *c, frame); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(a.Config.DataDir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.Config.DataDir, frame.StorageKey)
	if err = os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	article := domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "fixture", Sections: []domain.Section{{ID: "s", Title: "section", Blocks: []domain.Block{{ID: "figure", Type: "figure", FrameIDs: []string{frame.ID}}}}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
	if err = a.Store.Publish(ctx, *c, article); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	base := "/api/v1/requests/" + r.ID + "/frames/" + frame.ID
	content := get(base + "/content")
	content.Body.Close()
	if content.StatusCode != 503 {
		t.Fatal(content.StatusCode)
	}
	metadata := get(base)
	defer metadata.Body.Close()
	var view struct {
		Frame domain.Frame `json:"frame"`
	}
	if err = json.NewDecoder(metadata.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Frame.Availability != "unavailable" || view.Frame.URLs["full"] != nil {
		t.Fatalf("known missing file stayed ready: %+v", view.Frame)
	}
	var restores int
	if err = a.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind='source.restore'").Scan(&restores); err != nil || restores != 0 {
		t.Fatalf("image viewing started restoration: %d %v", restores, err)
	}
}
