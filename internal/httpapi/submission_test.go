package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/ratelimit"
	"vidlens/internal/testenv"
)

func TestSubmissionClientIPTrustBoundary(t *testing.T) {
	a := &Server{}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "198.51.100.5:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.2")
	if ip := a.clientIP(r); ip != "198.51.100.5" {
		t.Fatal("trusted untrusted peer header", ip)
	}
	a.Config.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8")}
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.99, 198.51.100.2, 10.0.0.5")
	if ip := a.clientIP(r); ip != "198.51.100.2" {
		t.Fatal("did not stop at first untrusted hop", ip)
	}
	r.RemoteAddr = "[::ffff:198.51.100.5]:5000"
	if ip := a.clientIP(r); ip != "198.51.100.5" {
		t.Fatal("mapped IPv4 bypasses IP limit", ip)
	}
}

func TestHTTPSubmissionLimitsCoverGenerationQuestionReplayAndOutage(t *testing.T) {
	server, a := apiTest(t)
	ctx := context.Background()
	o, err := redis.ParseURL(testenv.Require(t, "VIDLENS_TEST_REDIS_URL"))
	if err != nil {
		t.Fatal("test Redis URL invalid")
	}
	p := config.SubmissionPolicy{Session: config.TokenBucket{Burst: 1, Every: time.Hour}, IP: config.TokenBucket{Burst: 100, Every: time.Hour}, Global: config.TokenBucket{Burst: 100, Every: time.Hour}}
	l := ratelimit.New(o, config.SubmissionLimits{Generation: p, Question: p})
	c := l.Redis
	t.Cleanup(func() { _ = c.Close() })
	l.Prefix = "vidlens:test:{" + domain.ID() + "}:http:"
	t.Cleanup(func() {
		keys, _ := c.Keys(ctx, l.Prefix+"*").Result()
		if len(keys) > 0 {
			_ = c.Del(ctx, keys...).Err()
		}
	})
	a.SubmissionLimiter = l.Check
	token := "submission-test-cookie"
	owner, err := a.Store.NewSession(ctx, hashToken(token), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path, key, body string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		req.Header.Set("Origin", a.Config.Origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", a.csrf(token))
		req.Header.Set("Idempotency-Key", key)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			t.Fatalf("unexpected HTTP body %d", res.StatusCode)
		}
		return res, v
	}
	body := `{"video_url":"https://youtube.com/watch?v=abcdefghijk"}`
	first, view := do("POST", "/api/v1/requests", "first", body)
	if first.StatusCode != 201 {
		t.Fatal(first.StatusCode, view)
	}
	requestID := view["request"].(map[string]any)["id"].(string)
	if res, _ := do("POST", "/api/v1/requests", "first", body); res.StatusCode != 200 || res.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("generation replay hit limiter")
	}
	if res, _ := do("POST", "/api/v1/requests", "first", `{"video_url":"https://youtube.com/watch?v=bcdefghijkl"}`); res.StatusCode != 409 {
		t.Fatal("conflict masked by limiter", res.StatusCode)
	}
	if res, v := do("POST", "/api/v1/requests", "second", body); res.StatusCode != 429 || res.Header.Get("Retry-After") == "" || v["code"] != "RATE_LIMITED" {
		t.Fatal("missing HTTP rate response", res.StatusCode, v)
	}
	// Publish a local fixture through the real Store state transitions; no worker
	// runs, network media or paid model requests are involved.
	r, _ := a.Store.Request(ctx, owner.ID, requestID)
	claim, err := a.Store.Claim(ctx, *r.ActiveRunID, 1, "fixture")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "video", StorageKey: "fixture-video", SHA256: "fixture-video", MIME: "video/mp4", Bytes: 1}
	if err = a.Store.AddAsset(ctx, *claim, video); err != nil {
		t.Fatal(err)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: claim.Run.SourceID, VideoHash: video.SHA256, TextHash: "fixture-text", DurationMS: 1000, Source: domain.Source{SourceID: claim.Run.SourceID}, Metadata: domain.JSON(map[string]any{})}
	if _, err = a.Store.FinishSource(ctx, *claim, snap, nil, video.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = a.Store.Request(ctx, owner.ID, requestID)
	claim, err = a.Store.Claim(ctx, *r.ActiveRunID, 1, "fixture")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	if err = a.Store.Publish(ctx, *claim, domain.Article{SchemaVersion: 1, SnapshotID: snap.ID, Title: "fixture", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Quality: "standard"}); err != nil {
		t.Fatal(err)
	}
	con, _, err := a.Store.EnsureConversation(ctx, owner.ID, requestID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/conversations/" + con.ID + "/turns"
	res, v := do("POST", path, "question", `{"question":"explain"}`)
	if res.StatusCode != 201 {
		t.Fatal("question shares generation quota", res.StatusCode, v)
	}
	turn := v["turn"].(map[string]any)["id"].(string)
	if res, _ := do("POST", path, "question", `{"question":"explain"}`); res.StatusCode != 200 {
		t.Fatal("question replay limited", res.StatusCode)
	}
	if err = a.Store.CancelTurn(ctx, owner.ID, con.ID, turn); err != nil {
		t.Fatal(err)
	}
	storedTurn, _ := a.Store.Turn(ctx, owner.ID, con.ID, turn)
	if _, err = a.Store.Claim(ctx, storedTurn.RunID, 1, "cancel-fixture"); err != nil {
		t.Fatal(err)
	}
	if res, _ := do("POST", path, "next", `{"question":"next","expected_last_turn_id":"`+turn+`"}`); res.StatusCode != 429 {
		t.Fatal("new question bypassed guard", res.StatusCode)
	}
	if res, _ := do("POST", path+"/"+turn+"/retry", "retry", `{"expected_last_turn_id":"`+turn+`"}`); res.StatusCode != 429 {
		t.Fatal("retry bypassed guard", res.StatusCode)
	}
	if res, _ := do("POST", "/api/v1/requests/"+requestID+"/reviews", "review", `{}`); res.StatusCode != 429 {
		t.Fatal("manual review bypassed interactive guard", res.StatusCode)
	}
	a.SubmissionLimiter = nil
	a.Config.RedisURL = "configured"
	if res, _ := do("POST", "/api/v1/requests", "first", body); res.StatusCode != 200 {
		t.Fatal("known replay unavailable during guard outage", res.StatusCode)
	}
	if res, _ := do("POST", "/api/v1/requests", "new", body); res.StatusCode != 503 {
		t.Fatal("missing guard allowed new request", res.StatusCode)
	}
	if res, _ := do("GET", "/api/v1/requests/"+requestID, "", ""); res.StatusCode != 200 {
		t.Fatal("guard outage blocked existing read", res.StatusCode)
	}
}
