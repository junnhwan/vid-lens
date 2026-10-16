package store

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestNetworkConfigurationRetainsEncryptsAndClearsKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	fallback := config.Config{ReferenceReadEnabled: true, SearchEnabled: true, TavilyAPIKey: "yaml-secret", SearchMonthlyLimit: 1000}
	v, e := s.NetworkSettings(ctx, fallback)
	if e != nil || v.Revision != 0 || !v.KeyConfigured || v.DailyLimit != 20 {
		t.Fatalf("fallback: %+v %v", v, e)
	}
	input := NetworkInput{ReferenceReadEnabled: true, SearchEnabled: true, DailyLimit: 20, MonthlyLimit: 1000}
	v, e = s.SaveNetworkSettings(ctx, fallback, input)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "tavily:") {
		t.Fatal("settings exposed key material")
	}
	var raw []byte
	if e = s.Pool.QueryRow(ctx, "SELECT ciphertext FROM credentials WHERE name=$1", v.KeyRef).Scan(&raw); e != nil || strings.Contains(string(raw), "yaml-secret") {
		t.Fatal("key was not encrypted")
	}
	input.ExpectedRevision = v.Revision
	input.APIKey = "replacement-key"
	v, e = s.SaveNetworkSettings(ctx, fallback, input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveNetworkSettings(ctx, fallback, input); e == nil {
		t.Fatal("stale form overwrote configuration")
	}
	cfg, e := s.NetworkConfig(ctx, fallback)
	if e != nil || cfg.TavilyAPIKey != "replacement-key" {
		t.Fatal("save did not take effect")
	}
	input.ExpectedRevision = v.Revision
	input.APIKey = ""
	input.DailyLimit = 5
	v, e = s.SaveNetworkSettings(ctx, fallback, input)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e = s.NetworkConfig(ctx, fallback)
	if e != nil || cfg.TavilyAPIKey != "replacement-key" {
		t.Fatal("empty field erased key")
	}
	input.ExpectedRevision = v.Revision
	input.ClearKey = true
	input.SearchEnabled = false
	v, e = s.SaveNetworkSettings(ctx, fallback, input)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e = s.NetworkConfig(ctx, fallback)
	if e != nil || cfg.TavilyAPIKey != "" || v.KeyConfigured {
		t.Fatal("clear fell back to old YAML key")
	}
	input.ExpectedRevision = v.Revision
	input.ClearKey = false
	input.SearchEnabled = true
	if _, e = s.SaveNetworkSettings(ctx, fallback, input); e == nil {
		t.Fatal("enabled search without key")
	}
	input.SearchEnabled = false
	input.DailyLimit = -1
	if _, e = s.SaveNetworkSettings(ctx, fallback, input); e == nil {
		t.Fatal("accepted negative daily limit")
	}
}

func TestDailySearchQuotaIsSharedAtomicAndDoesNotResetWithKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	cfg := config.Config{SearchMonthlyLimit: 1000}
	input := NetworkInput{ReferenceReadEnabled: true, SearchEnabled: true, DailyLimit: 2, MonthlyLimit: 1000, APIKey: "fixture-key"}
	v, e := s.SaveNetworkSettings(ctx, cfg, input)
	if e != nil {
		t.Fatal(e)
	}
	claims := make([]domain.Claim, 4)
	_, _, first := referenceTurn(t, s)
	another := func() domain.Claim {
		t.Helper()
		owner := session(t, s)
		r, e := s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
		if e != nil {
			t.Fatal(e)
		}
		con, _, e := s.EnsureConversation(ctx, owner, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		turn, e := s.CreateTurn(ctx, owner, con.ID, domain.ID(), domain.QuestionInput{Question: "fixture", AllowExternalSources: true}, "")
		if e != nil {
			t.Fatal(e)
		}
		v, e := s.Turn(ctx, owner, con.ID, turn.ID)
		if e != nil {
			t.Fatal(e)
		}
		c, e := s.Claim(ctx, v.RunID, 1, "quota-test")
		if e != nil || c == nil {
			t.Fatalf("claim: %v", e)
		}
		return *c
	}
	for i := range claims {
		if i == 0 {
			claims[i] = first
		} else {
			claims[i] = another()
		}
		if _, e = s.BeginOperation(ctx, claims[i], "search", "web.search", map[string]string{"q": "fixture"}, false); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for _, c := range claims {
		wg.Add(1)
		go func(c domain.Claim) {
			defer wg.Done()
			if s.ReserveSearch(ctx, c, "search", 1000) == nil {
				accepted.Add(1)
			}
		}(c)
	}
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("daily quota admitted %d", accepted.Load())
	}
	input.ExpectedRevision = v.Revision
	input.APIKey = "new-key"
	v, e = s.SaveNetworkSettings(ctx, cfg, input)
	if e != nil {
		t.Fatal(e)
	}
	c := another()
	if _, e = s.BeginOperation(ctx, c, "next", "web.search", nil, false); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveSearch(ctx, c, "next", 1000); e == nil {
		t.Fatal("key change replenished daily quota")
	}
	// Yesterday's receipts remain in the monthly count, but do not consume today.
	if _, e = s.Pool.Exec(ctx, `UPDATE external_search_credits SET created_at=(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')-interval '1 second'`); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveSearch(ctx, c, "next", 1000); e != nil {
		t.Fatalf("Beijing daily reset failed: %v", e)
	}
	// Lower the cap to zero after the caller read a larger limit: DB wins.
	input.ExpectedRevision = v.Revision
	input.DailyLimit = 0
	input.APIKey = ""
	if _, e = s.SaveNetworkSettings(ctx, cfg, input); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginOperation(ctx, c, "zero", "web.search", nil, false); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveSearch(ctx, c, "zero", 1000); e == nil {
		t.Fatal("stale caller bypassed zero cap")
	}
}
