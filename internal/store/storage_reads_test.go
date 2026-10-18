package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vidlens/internal/domain"
)

// Pause a real query after its snapshot has been established. The writer uses
// another connection, so assertions do not depend on sleeps or query speed.
type storageReadGate struct {
	match   string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}
type storageReadGateKey struct{}

func (g *storageReadGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, storageReadGateKey{}, strings.Contains(data.SQL, g.match))
}
func (g *storageReadGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(storageReadGateKey{}).(bool); matched {
		g.once.Do(func() {
			close(g.entered)
			select {
			case <-g.release:
			case <-ctx.Done():
			}
		})
	}
}

func gatedStorageReader(t *testing.T, s *Store, match string) (*Store, *storageReadGate, func()) {
	t.Helper()
	g := &storageReadGate{match: match, entered: make(chan struct{}), release: make(chan struct{})}
	pc := s.Pool.Config()
	pc.ConnConfig.Tracer = g
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var once sync.Once
	release := func() { once.Do(func() { close(g.release) }) }
	t.Cleanup(release)
	reader := *s
	reader.Pool = pool
	return &reader, g, release
}

func waitForStorageRead(t *testing.T, g *storageReadGate) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("storage query did not reach snapshot barrier")
	}
}

func TestStorageReadsDoNotAcquireControlLock(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(867914031)"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := s.StorageOverview(ctx); err != nil {
		t.Errorf("overview waited for control lock: %v", err)
	}
	if _, _, err := s.ManagedVideos(ctx, "", 10, 0); err != nil {
		t.Errorf("video list waited for control lock: %v", err)
	}
}

func TestSlowStorageReadsDoNotBlockRunControl(t *testing.T) {
	for _, operation := range []string{"overview", "videos"} {
		t.Run(operation, func(t *testing.T) {
			s := testStore(t)
			s.VideoConcurrency = 4
			ctx := context.Background()
			_, running := prepared(t, s, session(t, s))
			owner := session(t, s)
			queued, err := s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=readclaim01", "", false)
			if err != nil {
				t.Fatal(err)
			}
			view, err := s.Request(ctx, owner, queued.ID)
			if err != nil {
				t.Fatal(err)
			}
			canceled, err := s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=readcancel1", "", false)
			if err != nil {
				t.Fatal(err)
			}
			match := "SELECT revision,video_quota_bytes"
			if operation == "videos" {
				match = "SELECT count(*) FROM filtered"
			}
			reader, gate, release := gatedStorageReader(t, s, match)
			done := make(chan error, 1)
			go func() {
				if operation == "overview" {
					_, err := reader.StorageOverview(ctx)
					done <- err
				} else {
					_, _, err := reader.ManagedVideos(ctx, "", 10, 0)
					done <- err
				}
			}()
			waitForStorageRead(t, gate)
			control, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if err := s.Heartbeat(control, running); err != nil {
				t.Fatal("slow read blocked heartbeat", err)
			}
			claim, err := s.Claim(control, *view.ActiveRunID, 1, "read-control")
			if err != nil || claim == nil {
				t.Fatalf("slow read blocked claim: %v", err)
			}
			if err := s.CancelRequest(control, owner, canceled.ID); err != nil {
				t.Fatal("slow read blocked cancellation", err)
			}
			release()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManagedVideosCountAndPageShareSnapshot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	if _, err := s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=snapshot001", "", false); err != nil {
		t.Fatal(err)
	}
	reader, gate, release := gatedStorageReader(t, s, "SELECT count(*) FROM filtered")
	type result struct {
		items []ManagedVideo
		total int64
		err   error
	}
	done := make(chan result, 1)
	go func() { items, total, err := reader.ManagedVideos(ctx, "", 10, 0); done <- result{items, total, err} }()
	waitForStorageRead(t, gate)
	control, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := s.CreateRequest(control, owner, domain.ID(), "https://youtube.com/watch?v=snapshot002", "", false); err != nil {
		t.Fatal("concurrent request blocked", err)
	}
	release()
	got := <-done
	if got.err != nil || got.total != 1 || len(got.items) != 1 {
		t.Fatalf("count/page snapshot diverged: total=%d items=%d err=%v", got.total, len(got.items), got.err)
	}
	items, total, err := s.ManagedVideos(ctx, "", 10, 0)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("subsequent read missed committed request: total=%d items=%d err=%v", total, len(items), err)
	}
}

func TestStorageOverviewSettingsAndLedgerShareSnapshot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, claim := prepared(t, s, session(t, s))
	before, err := s.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader, gate, release := gatedStorageReader(t, s, "SELECT revision,video_quota_bytes")
	type result struct {
		overview StorageOverview
		err      error
	}
	done := make(chan result, 1)
	go func() { overview, err := reader.StorageOverview(ctx); done <- result{overview, err} }()
	waitForStorageRead(t, gate)
	control, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.ReserveStorage(control, claim, 2<<30, IncomingFrameBytes, 100<<30, 200<<30, 85); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveStorageSettings(control, StorageInput{ExpectedRevision: before.Settings.Revision, VideoQuotaBytes: 6 << 30, ImageQuotaBytes: 2 << 30}, 3<<30); err != nil {
		t.Fatal(err)
	}
	release()
	got := <-done
	if got.err != nil || got.overview != before {
		t.Fatalf("overview mixed settings and ledger snapshots: %+v, err=%v", got.overview, got.err)
	}
	after, err := s.StorageOverview(ctx)
	if err != nil || after.Settings.Revision == before.Settings.Revision || after.ReservedVideoBytes != 2<<30 {
		t.Fatalf("subsequent overview missed committed changes: %+v, err=%v", after, err)
	}
}
