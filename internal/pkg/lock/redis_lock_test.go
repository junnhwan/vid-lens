package lock

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, redis.Cmdable) {
	t.Helper()
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return s, client
}

// 验证互斥性：A 持有锁时，B 无法获取。
// 这也是对 owner 用 UUID（而非时间戳）的回归保护——
// 时间戳方案在并发下会产生相同 owner，破坏互斥。
func TestRedisLockMutualExclusion(t *testing.T) {
	_, client := newTestRedis(t)
	ctx := context.Background()

	a := NewRedisLock(client, "vidlens:test:mutex")
	if ok, err := a.TryLock(ctx, 0); err != nil || !ok {
		t.Fatalf("A TryLock: ok=%v err=%v", ok, err)
	}
	defer a.Unlock(ctx)

	b := NewRedisLock(client, "vidlens:test:mutex")
	got, err := b.TryLock(ctx, 0)
	if err != nil {
		t.Fatalf("B TryLock err: %v", err)
	}
	if got {
		t.Fatalf("B 不应获取到 A 已持有的锁")
	}
}

// 验证释放时的 owner 校验：用错误的 owner 调 Unlock 不应删除别人的锁。
func TestRedisLockUnlockOnlyReleasesOwner(t *testing.T) {
	_, client := newTestRedis(t)
	ctx := context.Background()

	owner := NewRedisLock(client, "vidlens:test:owner")
	if ok, err := owner.TryLock(ctx, 0); err != nil || !ok {
		t.Fatalf("owner TryLock: ok=%v err=%v", ok, err)
	}
	defer owner.Unlock(ctx)

	// 伪造一个持有错误 owner 的实例，尝试释放——Lua 脚本应拒绝。
	intruder := &RedisLock{
		client: client,
		key:    "vidlens:test:owner",
		value:  "not-the-real-owner",
		ttl:    30 * time.Second,
	}
	if err := intruder.Unlock(ctx); err != nil {
		t.Fatalf("intruder Unlock: %v", err)
	}

	// 锁应仍被 owner 持有：新的竞争者拿不到
	contender := NewRedisLock(client, "vidlens:test:owner")
	if got, _ := contender.TryLock(ctx, 0); got {
		t.Fatalf("锁被错误 owner 误删了")
	}

	// owner 正常释放后，后续才能获取
	if err := owner.Unlock(ctx); err != nil {
		t.Fatalf("owner Unlock: %v", err)
	}
	next := NewRedisLock(client, "vidlens:test:owner")
	if got, _ := next.TryLock(ctx, 0); !got {
		t.Fatalf("owner 释放后应可重新获取")
	}
}

// 验证重复 Unlock 不会 panic（stopChan 置 nil 的 double-close 防护）。
func TestRedisLockDoubleUnlockNoPanic(t *testing.T) {
	_, client := newTestRedis(t)
	ctx := context.Background()

	l := NewRedisLock(client, "vidlens:test:dbl")
	if ok, _ := l.TryLock(ctx, 0); !ok {
		t.Fatalf("TryLock failed")
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}
	if err := l.Unlock(ctx); err != nil { // 不能 panic
		t.Fatalf("second Unlock: %v", err)
	}
}

// 验证续期能把 TTL 重置回 ttl，且续期后锁能在原本该过期的时间点仍存活。
func TestRedisLockRenewExtendsTTL(t *testing.T) {
	s, client := newTestRedis(t)
	ctx := context.Background()
	const key = "vidlens:test:renew"

	l := NewRedisLock(client, key)
	if ok, _ := l.TryLock(ctx, 0); !ok {
		t.Fatalf("TryLock failed")
	}
	defer l.Unlock(ctx)

	// 快进 20s（< ttl 30s），锁应仍在
	s.FastForward(20 * time.Second)
	if !s.Exists(key) {
		t.Fatalf("锁不应在 ttl 内过期")
	}

	// 手动续期，TTL 应回到 ~30s
	if !l.renew(ctx) {
		t.Fatalf("renew 应返回 true")
	}
	ttl := s.TTL(key)
	if ttl < 25*time.Second || ttl > 30*time.Second {
		t.Fatalf("续期后 TTL 应≈30s，got %v", ttl)
	}

	// 快进 25s：未续期会过期（30-20=10s 剩余），但刚续期过所以仍存活
	s.FastForward(25 * time.Second)
	if !s.Exists(key) {
		t.Fatalf("续期后锁不应在该时间点过期")
	}
}

// 验证 TryLock 的等待语义：锁释放后，等待方能在 waitTimeout 内获取到。
func TestRedisLockTryLockWaitsForRelease(t *testing.T) {
	_, client := newTestRedis(t)
	ctx := context.Background()

	holder := NewRedisLock(client, "vidlens:test:wait")
	if ok, _ := holder.TryLock(ctx, 0); !ok {
		t.Fatalf("holder TryLock failed")
	}

	done := make(chan struct{})
	var got bool
	go func() {
		defer close(done)
		waiter := NewRedisLock(client, "vidlens:test:wait")
		got, _ = waiter.TryLock(ctx, 2*time.Second)
		if got {
			waiter.Unlock(ctx)
		}
	}()

	// 让 waiter 先进入等待循环，再释放锁
	time.Sleep(150 * time.Millisecond)
	holder.Unlock(ctx)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("waiter 未在 waitTimeout 内返回")
	}
	if !got {
		t.Fatalf("waiter 应在 holder 释放后获取到锁")
	}
}
