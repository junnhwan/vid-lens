package ratelimit

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/testenv"
)

func testLimiter(t *testing.T, policy config.SubmissionPolicy) *Limiter {
	t.Helper()
	url := testenv.Require(t, "VIDLENS_TEST_REDIS_URL")
	o, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal("invalid test Redis URL")
	}
	l := New(o, config.SubmissionLimits{Generation: policy, Question: policy})
	c := l.Redis
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal("test Redis unavailable")
	}
	l.Prefix = "vidlens:test:{" + domain.ID() + "}:submission:"
	t.Cleanup(func() {
		keys, err := c.Keys(context.Background(), l.Prefix+"*").Result()
		if err == nil && len(keys) > 0 {
			_ = c.Del(context.Background(), keys...).Err()
		}
		_ = c.Close()
	})
	return l
}

func isLimited(err error) bool {
	var p *domain.Problem
	return errors.As(err, &p) && p.Status == 429 && p.Code == "RATE_LIMITED"
}

func TestSubmissionBucketsRefillAndExpire(t *testing.T) {
	b := config.TokenBucket{Burst: 1, Every: 50 * time.Millisecond}
	l := testLimiter(t, config.SubmissionPolicy{Session: b, IP: b, Global: b})
	ctx := context.Background()
	if err := l.Check(ctx, "generation", "session", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := l.Check(ctx, "generation", "session", "ip"); !isLimited(err) {
		t.Fatalf("missing burst limit: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if err := l.Check(ctx, "generation", "session", "ip"); err != nil {
		t.Fatal("bucket did not refill", err)
	}
	keys, _ := l.Redis.Keys(ctx, l.Prefix+"*").Result()
	for _, key := range keys {
		ttl, _ := l.Redis.PTTL(ctx, key).Result()
		if ttl <= 0 || ttl > 50*time.Millisecond {
			t.Fatal("bucket has unbounded lifetime", ttl)
		}
	}
}

func TestSubmissionDimensionsAreAtomicAndSeparate(t *testing.T) {
	big := config.TokenBucket{Burst: 100, Every: time.Hour}
	small := config.TokenBucket{Burst: 1, Every: time.Hour}
	ctx := context.Background()
	for _, dim := range []string{"session", "ip", "global"} {
		t.Run(dim, func(t *testing.T) {
			p := config.SubmissionPolicy{Session: big, IP: big, Global: big}
			switch dim {
			case "session":
				p.Session = small
			case "ip":
				p.IP = small
			case "global":
				p.Global = small
			}
			l := testLimiter(t, p)
			if err := l.Check(ctx, "generation", "a", "ip-a"); err != nil {
				t.Fatal(err)
			}
			session, ip := "a", "ip-a"
			if dim == "ip" {
				session = "b"
			}
			if dim == "global" {
				session, ip = "b", "ip-b"
			}
			before, _ := l.Redis.Keys(ctx, l.Prefix+"*").Result()
			states := map[string]string{}
			for _, key := range before {
				v, _ := l.Redis.HGetAll(ctx, key).Result()
				states[key] = string(domain.JSON(v))
			}
			if err := l.Check(ctx, "generation", session, ip); !isLimited(err) {
				t.Fatalf("dimension not enforced: %v", err)
			}
			after, _ := l.Redis.Keys(ctx, l.Prefix+"*").Result()
			if len(after) != len(before) {
				t.Fatal("denial created another dimension's bucket")
			}
			for _, key := range before {
				v, _ := l.Redis.HGetAll(ctx, key).Result()
				if string(domain.JSON(v)) != states[key] {
					t.Fatal("partial deduction on denial")
				}
			}
			if err := l.Check(ctx, "question", "a", "ip-a"); err != nil {
				t.Fatal("question and generation share a bucket", err)
			}
		})
	}
}

func TestSubmissionGlobalBucketAcrossConcurrentClients(t *testing.T) {
	p := config.SubmissionPolicy{Session: config.TokenBucket{Burst: 100, Every: time.Hour}, IP: config.TokenBucket{Burst: 100, Every: time.Hour}, Global: config.TokenBucket{Burst: 7, Every: time.Hour}}
	l := testLimiter(t, p)
	other := New(l.Redis.Options(), l.Limits)
	other.Prefix = l.Prefix
	t.Cleanup(func() { _ = other.Redis.Close() })
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := l
			if i%2 == 0 {
				client = other
			}
			err := client.Check(context.Background(), "generation", domain.ID(), domain.ID())
			if err == nil {
				admitted.Add(1)
			} else if !isLimited(err) {
				t.Errorf("unexpected admission error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 7 {
		t.Fatalf("overspent global tokens: admitted=%d", admitted.Load())
	}
}

func TestSubmissionGuardFailsClosedAndDoesNotStoreActorsInKeys(t *testing.T) {
	l := testLimiter(t, config.DefaultSubmissionLimits().Generation)
	if err := l.Check(context.Background(), "generation", "private-session-value", "198.51.100.99"); err != nil {
		t.Fatal(err)
	}
	keys, _ := l.Redis.Keys(context.Background(), l.Prefix+"*").Result()
	for _, key := range keys {
		if strings.Contains(key, "private-session-value") || strings.Contains(key, "198.51.100.99") {
			t.Fatal("raw actor stored in key")
		}
	}
	unavailable := New(l.Redis.Options(), l.Limits)
	_ = unavailable.Redis.Close()
	var p *domain.Problem
	if err := unavailable.Check(context.Background(), "generation", "a", "b"); !errors.As(err, &p) || p.Status != 503 {
		t.Fatal("Redis failure allowed submission", err)
	}
}

func TestSubmissionGuardBoundsAnUnresponsiveRedis(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = connection.Close() })
		}
	}()
	l := New(&redis.Options{Addr: listener.Addr().String(), ReadTimeout: 5 * time.Second}, config.DefaultSubmissionLimits())
	client := l.Redis
	t.Cleanup(func() { _ = client.Close() })
	start := time.Now()
	err = l.Check(context.Background(), "generation", "a", "b")
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Status != 503 {
		t.Fatal("unresponsive Redis did not reject submission", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatal("guard held the admission transaction too long", elapsed)
	}
}

type dropEvalReplyConn struct {
	net.Conn
	writes  *atomic.Int32
	dropped *atomic.Bool
	armed   atomic.Bool
}

func (c *dropEvalReplyConn) Write(data []byte) (int, error) {
	if strings.Contains(strings.ToLower(string(data)), "\r\nevalsha\r\n") {
		c.writes.Add(1)
		c.armed.Store(true)
	}
	return c.Conn.Write(data)
}

func (c *dropEvalReplyConn) Read(data []byte) (int, error) {
	n, err := c.Conn.Read(data)
	if n > 0 && c.armed.Load() && c.dropped.CompareAndSwap(false, true) {
		_ = c.Conn.Close()
		return 0, io.EOF // Redis executed the script; the caller lost its reply.
	}
	return n, err
}

func TestSubmissionUnknownRedisOutcomeIsNotAutomaticallyDeductedTwice(t *testing.T) {
	b := config.TokenBucket{Burst: 10, Every: time.Hour}
	l := testLimiter(t, config.SubmissionPolicy{Session: b, IP: b, Global: b})
	ctx := context.Background()
	if err := takeTokens.Load(ctx, l.Redis).Err(); err != nil {
		t.Fatal(err)
	}
	options := *l.Redis.Options()
	// Options exposes the normalized zero (disabled), whereas NewClient treats
	// an input zero as its default retry count. Preserve the original policy.
	if options.MaxRetries == 0 {
		options.MaxRetries = -1
	}
	dial := options.Dialer
	var writes atomic.Int32
	var dropped atomic.Bool
	options.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &dropEvalReplyConn{Conn: conn, writes: &writes, dropped: &dropped}, nil
	}
	fault := &Limiter{Redis: redis.NewClient(&options), Limits: l.Limits, Prefix: l.Prefix}
	t.Cleanup(func() { _ = fault.Redis.Close() })
	var problem *domain.Problem
	if err := fault.Check(ctx, "generation", "actor", "ip"); !errors.As(err, &problem) || problem.Status != 503 {
		t.Fatal("unknown deduction should reject the attempt", err, "writes", writes.Load(), "reply_dropped", dropped.Load())
	}
	if !dropped.Load() || writes.Load() != 1 {
		t.Fatal("unknown EVAL was automatically replayed", writes.Load())
	}
	keys, err := l.Redis.Keys(ctx, l.Prefix+"*").Result()
	if err != nil || len(keys) != 3 {
		t.Fatal("script was not executed before losing the reply", err)
	}
	for _, key := range keys {
		if tokens, err := l.Redis.HGet(ctx, key, "tokens").Result(); err != nil || tokens != "9" {
			t.Fatal("unknown outcome spent more than one token", tokens, err)
		}
	}
}
