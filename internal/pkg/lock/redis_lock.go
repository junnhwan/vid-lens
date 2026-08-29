package lock

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisLock 基于 Redis 的分布式锁，带看门狗自动续期。
//
// 每个 RedisLock 实例只管理一次成功获取的 lease。lease 持有不可变 owner，
// Watchdog 只读取启动时传入的 lease，不读取 RedisLock 上会被 Unlock 修改的字段。
// Redis 侧的续期和释放都通过 Lua 校验 owner，避免误操作其他持有者的锁。
type RedisLock struct {
	client redis.Cmdable
	key    string
	ttl    time.Duration

	mu    sync.Mutex
	lease *redisLockLease
}

type redisLockLease struct {
	owner    string
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// NewRedisLock 创建分布式锁实例。
func NewRedisLock(client redis.Cmdable, key string) *RedisLock {
	return &RedisLock{
		client: client,
		key:    key,
		ttl:    30 * time.Second,
	}
}

// TryLock 尝试获取锁。
// waitTimeout 为等待获取锁的最大时间；0 表示只尝试一次。
func (l *RedisLock) TryLock(ctx context.Context, waitTimeout time.Duration) (bool, error) {
	l.mu.Lock()
	alreadyHeld := l.lease != nil
	l.mu.Unlock()
	if alreadyHeld {
		return false, fmt.Errorf("redis lock instance already holds key %s", l.key)
	}

	owner := uuid.NewString()
	deadline := time.Now().Add(waitTimeout)
	for {
		ok, err := l.client.SetNX(ctx, l.key, owner, l.ttl).Result()
		if err != nil {
			return false, fmt.Errorf("获取锁失败: %w", err)
		}
		if ok {
			lease := &redisLockLease{
				owner: owner,
				stop:  make(chan struct{}),
				done:  make(chan struct{}),
			}
			l.mu.Lock()
			l.lease = lease
			l.mu.Unlock()
			go l.watchdog(ctx, lease)
			return true, nil
		}

		if time.Now().After(deadline) {
			return false, nil
		}

		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

// watchdog 每隔 ttl/3 续期一次，直到 lease 被释放或父 context 结束。
func (l *RedisLock) watchdog(ctx context.Context, lease *redisLockLease) {
	defer close(lease.done)
	ticker := time.NewTicker(l.ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.renewLease(ctx, lease)
		case <-lease.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// renew 为测试和同步调用保留；Watchdog 使用 renewLease，避免重新读取可变 lease。
func (l *RedisLock) renew(ctx context.Context) bool {
	l.mu.Lock()
	lease := l.lease
	l.mu.Unlock()
	if lease == nil {
		return false
	}
	return l.renewLease(ctx, lease)
}

func (l *RedisLock) renewLease(ctx context.Context, lease *redisLockLease) bool {
	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("expire", KEYS[1], ARGV[2])
	else
		return 0
	end`
	res, err := l.client.Eval(ctx, script, []string{l.key}, lease.owner, int(l.ttl.Seconds())).Result()
	if err != nil {
		log.Printf("[redis_lock] 续期失败 key=%s owner=%s err=%v", l.key, lease.owner, err)
		return false
	}
	n, _ := res.(int64)
	if n == 0 {
		log.Printf("[redis_lock] 续期未生效：锁已不归当前持有者 key=%s owner=%s", l.key, lease.owner)
	}
	return n != 0
}

// Unlock 停止当前 lease 的 Watchdog，并使用 owner 校验 Lua 安全释放 Redis 锁。
// 多个 goroutine 并发调用 Unlock 时，只有取到 lease 的调用会执行释放，其余调用幂等返回。
func (l *RedisLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	lease := l.lease
	l.lease = nil
	l.mu.Unlock()
	if lease == nil {
		return nil
	}

	lease.stopOnce.Do(func() { close(lease.stop) })
	<-lease.done

	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("del", KEYS[1])
	else
		return 0
	end`
	_, err := l.client.Eval(ctx, script, []string{l.key}, lease.owner).Result()
	return err
}
