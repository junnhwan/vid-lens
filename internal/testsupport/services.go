package testsupport

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/resource"
	"vidlens/internal/store"
)

func Services(t *testing.T) (*store.Store, *resource.Manager, config.Config) {
	t.Helper()
	dsn := os.Getenv("VIDLENS_TEST_DATABASE_URL")
	redis := os.Getenv("VIDLENS_TEST_REDIS_URL")
	if dsn == "" || redis == "" {
		t.Skip("set isolated VIDLENS_TEST_DATABASE_URL and VIDLENS_TEST_REDIS_URL")
	}
	ctx := context.Background()
	secret := strings.Repeat("test-secret-", 5)
	base, e := store.Open(ctx, dsn, secret)
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
	s, e := store.Open(ctx, u.String(), secret)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	p := config.DefaultProfile()
	p.ModelBaseURL = "http://example.test/v1"
	p.ModelName = "test"
	if e = s.Bootstrap(ctx, p, "test", ""); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{DatabaseURL: u.String(), RedisURL: redis, SecretKey: secret, DataDir: t.TempDir(), DevMediaDir: t.TempDir(), DiskStopPercent: 99, Profile: p}
	r, e := resource.New(s, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Redis.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		r.Redis.Close()
		s.Pool.Close()
		_, _ = base.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		base.Pool.Close()
	})
	return s, r, cfg
}
