package store

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("VIDLENS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VIDLENS_TEST_DATABASE_URL to an isolated PostgreSQL database")
	}
	ctx := context.Background()
	base, e := Open(ctx, dsn, "test-secret-long-enough-to-encrypt-credentials")
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
	s, e := Open(ctx, u.String(), "test-secret-long-enough-to-encrypt-credentials")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	p := config.DefaultProfile()
	p.ModelBaseURL = "https://example.test/v1"
	p.ModelName = "test"
	if e = s.Bootstrap(ctx, p, "secret", "asr"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.Pool.Close()
		_, _ = base.Pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		base.Pool.Close()
	})
	return s
}
func session(t *testing.T, s *Store) string {
	t.Helper()
	v, e := s.NewSession(context.Background(), domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	return v.ID
}
func TestSharedCancelFreezesHistoryAndIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := session(t, s), session(t, s)
	url := "https://www.bilibili.com/video/BV1xx411c7mD?p=1"
	x, e := s.CreateRequest(ctx, a, "same", url, "", false)
	if e != nil {
		t.Fatal(e)
	}
	y, e := s.CreateRequest(ctx, b, "other", url, "", false)
	if e != nil {
		t.Fatal(e)
	}
	ra, _ := s.Request(ctx, a, x.ID)
	rb, _ := s.Request(ctx, b, y.ID)
	if *ra.ActiveRunID != *rb.ActiveRunID {
		t.Fatal("work was not shared")
	}
	re, e := s.CreateRequest(ctx, a, "same", url, "", false)
	if e != nil || !re.Replayed || re.ID != x.ID {
		t.Fatalf("replay: %+v %v", re, e)
	}
	if _, e = s.CreateRequest(ctx, a, "same", url+"x", "", false); e == nil {
		t.Fatal("different body accepted")
	}
	claim, e := s.Claim(ctx, *ra.ActiveRunID, 1, "worker")
	if e != nil || claim == nil {
		t.Fatalf("claim %v", e)
	}
	step, e := s.Step(ctx, *claim, domain.Step{ID: domain.ID(), Title: "取得画面", Phase: "prepare_source", Status: "running"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CancelRequest(ctx, a, x.ID); e != nil {
		t.Fatal(e)
	}
	step.Status = "completed"
	if _, e = s.Step(ctx, *claim, step); e != nil {
		t.Fatal(e)
	}
	v, _, e := s.RequestView(ctx, a, x.ID)
	if e != nil {
		t.Fatal(e)
	}
	if v["execution"] != nil {
		t.Fatal("canceled request kept active execution")
	}
	act := v["activity"].(map[string]any)
	st := act["recent_steps"].([]domain.Step)
	if len(st) != 1 || st[0].Status != "running" {
		t.Fatalf("canceled history changed: %+v", st)
	}
	r, _ := s.Run(ctx, claim.Run.ID)
	if r.Status != "running" {
		t.Fatal("one cancellation stopped shared run")
	}
	rr, e := s.CreateRequest(ctx, a, "retry", "", x.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	re, e = s.CreateRequest(ctx, a, "retry", "", x.ID, false)
	if e != nil || !re.Replayed || re.ID != rr.ID {
		t.Fatalf("retry replay %+v %v", re, e)
	}
}

func TestCredentialRotationPreservesFrozenRevisionAndUsesOwnedReferences(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	oldID, old, err := s.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.ModelName = "next-model"
	updated.ModelKeyRef = "caller-supplied-ref"
	updated.ASRKeyRef = "caller-supplied-asr-ref"
	newID, err := s.SaveConfig(ctx, updated, "next-model-key", "next-asr-key")
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Profile(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	oldAgain, err := s.Profile(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := s.Credential(ctx, oldAgain.ModelKeyRef)
	if err != nil {
		t.Fatal(err)
	}
	nextKey, err := s.Credential(ctx, next.ModelKeyRef)
	if err != nil {
		t.Fatal(err)
	}
	if oldKey != "secret" || nextKey != "next-model-key" || next.ModelKeyRef == old.ModelKeyRef || next.ModelKeyRef == updated.ModelKeyRef {
		t.Fatal("credential rotation changed old work or honored an unowned reference")
	}
	id, err := s.SaveConfig(ctx, updated, "", "")
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := s.Profile(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.ModelKeyRef != next.ModelKeyRef || preserved.ASRKeyRef != next.ASRKeyRef || oldAgain.ModelName != "test" {
		t.Fatal("empty credential update or profile rewrite changed frozen authentication")
	}
}
func TestClaimFencingAndConcurrentAdmission(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	x, e := s.CreateRequest(ctx, owner, "one", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request(ctx, owner, x.ID)
	c1, e := s.Claim(ctx, *r.ActiveRunID, 1, "old")
	if e != nil || c1 == nil {
		t.Fatal(e)
	}
	_, e = s.Pool.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1", c1.Run.ID)
	if e != nil {
		t.Fatal(e)
	}
	c2, e := s.Claim(ctx, c1.Run.ID, 1, "new")
	if e != nil || c2 == nil {
		t.Fatal(e)
	}
	if e = s.Checkpoint(ctx, *c1, map[string]any{"late": true}); !errors.Is(e, domain.ErrFenced) {
		t.Fatalf("stale write: %v", e)
	}
	if e = s.Checkpoint(ctx, *c2, map[string]any{"schema_version": 1}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v="+domain.ID()[:11], "", false)
		}()
	}
	wg.Wait()
	var n int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE status NOT IN ('failed','succeeded','canceled')").Scan(&n)
	if n != 3 {
		t.Fatalf("admission count=%d", n)
	}
}
func TestCursorIsolation(t *testing.T) {
	s := testStore(t)
	p := Position{Session: "one", Type: "request", ID: "resource", Revision: 1, Seq: 2}
	raw := s.Cursor(p)
	if _, e := s.ParseCursor(raw, "two", "request", "resource"); e == nil {
		t.Fatal("cross-session cursor accepted")
	}
	if _, e := s.ParseCursor(raw+"x", "one", "request", "resource"); e == nil {
		t.Fatal("tampered cursor accepted")
	}
}
