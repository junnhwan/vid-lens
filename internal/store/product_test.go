package store

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
	"vidlens/internal/domain"
)

func modelInput(m CatalogModel) ModelInput {
	return ModelInput{ProviderID: m.ProviderID, DisplayName: m.DisplayName, BrandID: m.BrandID, Description: m.Description, UpstreamID: m.UpstreamID, Enabled: m.Enabled, ExpectedRevision: m.Revision, MaxOutputTokens: m.MaxOutputTokens, MaxInputChars: m.MaxInputChars, DisableThinking: m.DisableThinking, CachePolicy: m.CachePolicy}
}
func enabledModel(t *testing.T, s *Store, name string) CatalogModel {
	t.Helper()
	ctx := context.Background()
	p, e := s.SaveProvider(ctx, "", ProviderInput{Name: name, BaseURL: "https://" + name + ".example/v1", APIKey: name + "-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	m, e := s.SaveModel(ctx, "", ModelInput{ProviderID: p.ID, DisplayName: name, UpstreamID: name, BrandID: "generic", MaxInputChars: 180000, MaxOutputTokens: 10000})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", domain.ID())
	if e != nil {
		t.Fatal(e)
	}
	probe, e := s.ClaimProbe(ctx, x.ID)
	if e != nil || probe == nil {
		t.Fatal(e)
	}
	if e = s.FinishProbe(ctx, *probe, map[string]bool{"test_fixture_only": true}, ""); e != nil {
		t.Fatal(e)
	}
	in := modelInput(m)
	in.Enabled = true
	m, e = s.SaveModel(ctx, m.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestPurposeModelIsolationAndSharedPreparation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := session(t, s), session(t, s)
	first := enabledModel(t, s, "model-a")
	second := enabledModel(t, s, "model-b")
	var rev int64
	_ = s.Pool.QueryRow(ctx, "SELECT revision FROM model_settings").Scan(&rev)
	if e := s.SetDefaultModel(ctx, first.ID, rev); e != nil {
		t.Fatal(e)
	}
	in := domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", Purpose: "  观察步骤\r\n保留条件  ", ModelID: first.ID, ModelRevisionID: first.RevisionID}
	x, e := s.CreateRequestWithInput(ctx, a, "frozen", in, "", false)
	if e != nil {
		t.Fatal(e)
	}
	ra, _ := s.Request(ctx, a, x.ID)
	in.ModelID = second.ID
	in.ModelRevisionID = second.RevisionID
	y, e := s.CreateRequestWithInput(ctx, b, "other", in, "", false)
	if e != nil {
		t.Fatal(e)
	}
	rb, _ := s.Request(ctx, b, y.ID)
	if !sameID(ra.ActiveRunID, rb.ActiveRunID) {
		t.Fatal("changing interpretation model repeated source preparation")
	}
	if ra.GenerationSpec.Purpose != "观察步骤\n保留条件" || ra.GenerationSpec.Scope == rb.GenerationSpec.Scope {
		t.Fatal("purpose normalization or private scope lost")
	}
	// Two callers with identical custom purpose and model still cannot join
	// the same interpretation, but they share the independent source work.
	in.ModelID = first.ID
	in.ModelRevisionID = first.RevisionID
	z, e := s.CreateRequestWithInput(ctx, b, "same-purpose", in, "", false)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Claim(ctx, *ra.ActiveRunID, 1, "prepare")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fixture", MIME: "video/mp4", Bytes: 1, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if e = s.AddAsset(ctx, *c, video); e != nil {
		t.Fatal(e)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "fixture", TextHash: "text", DurationMS: 1000, Source: domain.Source{SourceID: c.Run.SourceID, CanonicalURL: in.VideoURL}, Metadata: []byte(`{}`)}
	if _, e = s.FinishSource(ctx, *c, snap, []domain.Segment{}, video.ID); e != nil {
		t.Fatal(e)
	}
	ra, _ = s.Request(ctx, a, x.ID)
	rb, _ = s.Request(ctx, b, y.ID)
	rc, _ := s.Request(ctx, b, z.ID)
	if sameID(ra.ActiveRunID, rb.ActiveRunID) || sameID(ra.ActiveRunID, rc.ActiveRunID) {
		t.Fatal("models/private purposes shared an article run")
	}
	// Idempotency replay precedes current catalog availability/default checks.
	mi := modelInput(first)
	mi.Enabled = false
	if _, e = s.SaveModel(ctx, first.ID, mi); e != nil {
		t.Fatal(e)
	}
	replay, e := s.CreateRequestWithInput(ctx, a, "frozen", in, "", false)
	if e != nil || replay.ID != x.ID || !replay.Replayed {
		t.Fatalf("original request replay changed: %+v %v", replay, e)
	}
	in.Purpose = "different"
	if _, e = s.CreateRequestWithInput(ctx, a, "frozen", in, "", false); e == nil {
		t.Fatal("same key accepted a changed purpose")
	}
	if e = s.CheckModelAvailable(ctx, ra.ConfigID); e == nil {
		t.Fatal("disabled model allowed another physical submission")
	}
}
func TestStatisticsAndLikesAreDurableAndIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Source: domain.Source{CanonicalURL: "https://example.com/video"}, Title: "fixture", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if _, _, e := s.RequestView(ctx, owner, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.SiteLike(ctx, owner, true); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	stats, e := s.SiteStats(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if stats["completed_readings"] != float64(1) || stats["likes"] != float64(1) {
		t.Fatalf("fake or duplicate counts: %+v", stats)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE anonymous_sessions SET expires_at=now()-interval '1 second' WHERE id=$1", owner); e != nil {
		t.Fatal(e)
	}
	after, _ := s.SiteStats(ctx)
	if after["likes"] != float64(1) {
		t.Fatal("session expiry erased cumulative facts")
	}
}
func TestCatalogRejectsUnverifiedAndStaleRevisions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p, e := s.SaveProvider(ctx, "", ProviderInput{Name: "provider", BaseURL: "https://example.com/v1", APIKey: "never-public-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	m, e := s.SaveModel(ctx, "", ModelInput{ProviderID: p.ID, DisplayName: "test", UpstreamID: "test", BrandID: "generic"})
	if e != nil {
		t.Fatal(e)
	}
	in := modelInput(m)
	in.Enabled = true
	if _, e = s.SaveModel(ctx, m.ID, in); e == nil {
		t.Fatal("unverified model became selectable")
	}
	if _, _, e = selectable(ctx, s.Pool, m.ID, "stale"); e == nil {
		t.Fatal("stale revision accepted")
	}
	if _, e = s.SaveProvider(ctx, p.ID, ProviderInput{Name: "changed", BaseURL: p.BaseURL, Enabled: true, ExpectedRevision: p.Revision + 1}); e == nil {
		t.Fatal("provider update overwrote newer revision")
	}
	public, e := s.ModelCatalog(ctx)
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(public)
	if string(wire) == "" || bytesContain(wire, "never-public-secret") || bytesContain(wire, "base_url") {
		t.Fatal("catalog leaked private routing or key")
	}
}
func bytesContain(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
func TestOwnerPasswordResetRevokesSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.SetOwnerPassword(ctx, "fixture-password-123"); e != nil {
		t.Fatal(e)
	}
	h, version, e := s.BeginOwnerLogin(ctx, "fixture-source")
	if e != nil || !s.VerifyOwnerPassword("fixture-password-123", h) || s.VerifyOwnerPassword("wrong", h) {
		t.Fatalf("password verification: %v", e)
	}
	if e = s.NewAdminSession(ctx, "fixture-source", "token-hash", version); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AdminSession(ctx, "token-hash"); e != nil {
		t.Fatal(e)
	}
	if e = s.SetOwnerPassword(ctx, "new-fixture-password"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AdminSession(ctx, "token-hash"); e == nil {
		t.Fatal("old session survived password reset")
	}
	if e = s.NewAdminSession(ctx, "fixture-source", "new-token", version); e == nil {
		t.Fatal("racing old-version login survived reset")
	}
}

func TestProviderKeyRotationCreatesDisabledModelRevisionWithoutRewritingOldConfig(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := enabledModel(t, s, "rotation")
	var oldCID string
	if e := s.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE id=$1", m.RevisionID).Scan(&oldCID); e != nil {
		t.Fatal(e)
	}
	before, e := s.Profile(ctx, oldCID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Provider(ctx, m.ProviderID)
	if e != nil {
		t.Fatal(e)
	}
	pi := ProviderInput{Name: p.Name, BaseURL: p.BaseURL, Enabled: false, ExpectedRevision: p.Revision, ProxyPolicy: p.ProxyPolicy}
	p, e = s.SaveProvider(ctx, p.ID, pi)
	if e != nil {
		t.Fatal(e)
	}
	afterToggle, _ := s.Model(ctx, m.ID)
	if afterToggle.RevisionID != m.RevisionID {
		t.Fatal("enable toggle rewrote effective model revision")
	}
	pi.ExpectedRevision = p.Revision
	pi.Enabled = true
	pi.APIKey = "rotated-secret"
	if _, e = s.SaveProvider(ctx, p.ID, pi); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Model(ctx, m.ID)
	if after.RevisionID == m.RevisionID || after.Enabled || after.Verified {
		t.Fatal("credential change retained a selectable or verified model revision")
	}
	old, e := s.Credential(ctx, before.ModelKeyRef)
	if e != nil || old != "rotation-secret" {
		t.Fatal("rotation overwrote a frozen credential")
	}
	current, e := s.Profile(ctx, oldCID)
	if e != nil || Hash(current) != Hash(before) {
		t.Fatal("rotation rewrote immutable configuration")
	}
}

func TestProbeRecoveryRetainsUnknownSubmissionAndOriginalIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := enabledModel(t, s, "probe-recovery")
	a, e := s.CreateProbe(ctx, m.ID, m.RevisionID, "cache", "durable-key")
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.ClaimProbe(ctx, a.ID)
	if e != nil || p == nil {
		t.Fatal(e)
	}
	if n, e := s.BeginProbeSubmission(ctx, *p); e != nil || n != 1 {
		t.Fatal("physical submission was not reserved")
	}
	if e = s.TouchProbe(ctx, *p); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PendingProbes(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE model_probes SET updated_at=now()-interval '61 seconds' WHERE id=$1", p.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PendingProbes(ctx); e != nil {
		t.Fatal(e)
	}
	var status string
	var submissions int
	if e = s.Pool.QueryRow(ctx, "SELECT status,submissions FROM model_probes WHERE id=$1", p.ID).Scan(&status, &submissions); e != nil || status != "unknown" || submissions != 1 {
		t.Fatal("crash silently reset the probe budget or outcome")
	}
	if again, e := s.ClaimProbe(ctx, p.ID); e != nil || again != nil {
		t.Fatal("unknown probe was automatically resubmitted")
	}
	if replay, e := s.CreateProbe(ctx, m.ID, m.RevisionID, "cache", "durable-key"); e != nil || replay.ID != a.ID || !replay.Replayed {
		t.Fatal("lost HTTP response created a new paid probe")
	}
}
