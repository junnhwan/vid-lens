package testsupport

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/resource"
	"vidlens/internal/store"
	"vidlens/internal/testenv"
)

func Services(t *testing.T) (*store.Store, *resource.Manager, config.Config) {
	t.Helper()
	dsn := testenv.Require(t, "VIDLENS_TEST_DATABASE_URL")
	redis := testenv.Require(t, "VIDLENS_TEST_REDIS_URL")
	ctx := context.Background()
	secret := strings.Repeat("test-secret-", 5)
	base, e := store.Open(ctx, dsn, secret)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(base.Pool.Close)
	schema := "test_" + domain.ID()
	if _, e = base.Pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = base.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, e := store.Open(ctx, u.String(), secret)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Pool.Close)
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	p := config.DefaultProfile()
	// Historical adapter fixtures explicitly select their contract; current
	// article-loop fixtures override it before admission.
	p.ArticleHarness = domain.ExecutionContractV12
	p.ModelBaseURL = "http://example.test/v1"
	p.ModelName = "test"
	if e = s.Bootstrap(ctx, p, "test", ""); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{DatabaseURL: u.String(), RedisURL: redis, SecretKey: secret, DataDir: t.TempDir(), DevMediaDir: t.TempDir(), Profile: p}
	r, e := resource.New(s, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Redis.Close() })
	if e = r.Redis.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	return s, r, cfg
}
