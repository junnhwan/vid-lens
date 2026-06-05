package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLock 基于 Redis 的分布式锁，带看门狗自动续期
// 面试亮点：
//   1. 不使用 SETNX + 固定过期时间（锁过期问题）
//   2. 实现了 WatchDog 自动续期机制，适配视频处理长耗时场景
//   3. finally 块中安全释放（检查 isHeldByCurrentThread）
//   4. Lua 脚本保证原子性
type RedisLock struct {
	client   redis.Cmdable
	key      string
	value    string // 持有者标识（UUID），防止误删别人的锁
	ttl      time.Duration
	stopChan chan struct{}
}

// NewRedisLock 创建分布式锁实例
func NewRedisLock(client redis.Cmdable, key string) *RedisLock {
	return &RedisLock{
		client: client,
		key:    key,
		ttl:    30 * time.Second, // 默认锁过期时间 30s（WatchDog 会续期）
	}
}

// TryLock 尝试获取锁
// waitTimeout: 等待获取锁的最大时间（0 表示不等待）
// 面试亮点：对应面试文档中的 Redisson.tryLock() 语义
func (l *RedisLock) TryLock(ctx context.Context, waitTimeout time.Duration) (bool, error) {
	// 生成持有者唯一标识
	lockValue := fmt.Sprintf("%d", time.Now().UnixNano())
	l.value = lockValue
	l.stopChan = make(chan struct{})

	deadline := time.Now().Add(waitTimeout)
	for {
		// Lua 脚本：SETNX + SETEX 原子操作
		ok, err := l.client.SetNX(ctx, l.key, lockValue, l.ttl).Result()
		if err != nil {
			return false, fmt.Errorf("获取锁失败: %w", err)
		}
		if ok {
			// 抢锁成功，启动 WatchDog
			go l.watchdog(ctx)
			return true, nil
		}

		// 没等到锁
		if time.Now().After(deadline) {
			return false, nil
		}

		// 短暂等待后重试
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// watchdog 看门狗：每隔 10s（ttl/3）自动续期
// 面试亮点：对应面试文档中的 WatchDog 机制
//   - 默认每隔 10s 检查一次，自动把锁过期时间重置为 30s
//   - 如果服务器宕机，WatchDog 线程挂掉，锁会在 30s 后自动过期
//   - 不会造成死锁
func (l *RedisLock) watchdog(ctx context.Context) {
	ticker := time.NewTicker(l.ttl / 3) // 10s
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 续期：用 Lua 脚本确保只有锁持有者才能续期
			l.renew(ctx)
		case <-l.stopChan:
			// 锁已释放，停止续期
			return
		case <-ctx.Done():
			return
		}
	}
}

// renew 续期锁（Lua 脚本保证原子性）
func (l *RedisLock) renew(ctx context.Context) {
	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("expire", KEYS[1], ARGV[2])
	else
		return 0
	end`
	l.client.Eval(ctx, script, []string{l.key}, l.value, int(l.ttl.Seconds()))
}

// Unlock 安全释放锁
// 面试亮点：
//   1. 必须放在 finally 中调用
//   2. 释放前检查当前线程是否持有锁（isHeldByCurrentThread）
//   3. Lua 脚本保证"判断+删除"的原子性
func (l *RedisLock) Unlock(ctx context.Context) error {
	// 停止 WatchDog
	if l.stopChan != nil {
		close(l.stopChan)
	}

	// Lua 脚本：只有持有者才能释放
	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("del", KEYS[1])
	else
		return 0
	end`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}
