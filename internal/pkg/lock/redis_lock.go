package lock

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisLock 基于 Redis 的分布式锁，带看门狗自动续期
// 设计要点：
//  1. 不使用 SETNX + 固定过期时间（锁过期问题），改用 WatchDog 自动续期
//  2. owner 用 UUID 标识，配合 Lua 脚本保证"只有持有者才能释放/续期"，防止误删别人的锁
//  3. Unlock 前经 Lua 脚本校验持有者（isHeldByCurrentThread 语义），避免误释放
//  4. 续期与释放均用 Lua 脚本保证原子性
//
// 注意：RedisLock 是有状态的（value/stopChan 在获取锁时写入），
// 不可重入、不可跨 goroutine 共享，每次加锁必须 NewRedisLock 新建实例。
type RedisLock struct {
	client   redis.Cmdable
	key      string
	value    string // 持有者标识（UUID），获取锁时生成，防止误删别人的锁
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
// waitTimeout: 等待获取锁的最大时间（0 表示不等待，只试一次）
// 语义参考 Redisson.tryLock()。
func (l *RedisLock) TryLock(ctx context.Context, waitTimeout time.Duration) (bool, error) {
	// 持有者标识必须用 UUID，不能用时间戳——
	// 高并发下同纳秒获取锁会产生相同 owner，导致释放脚本误删别人的锁，破坏互斥性。
	lockValue := uuid.New().String()
	l.value = lockValue
	l.stopChan = make(chan struct{})

	deadline := time.Now().Add(waitTimeout)
	for {
		// SetNX 是单命令原子操作，同时携带 TTL，等价于 SET key value NX EX 30
		ok, err := l.client.SetNX(ctx, l.key, lockValue, l.ttl).Result()
		if err != nil {
			return false, fmt.Errorf("获取锁失败: %w", err)
		}
		if ok {
			// 抢锁成功，启动 WatchDog
			go l.watchdog(ctx)
			return true, nil
		}

		// 没抢到
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

// watchdog 看门狗：每隔 ttl/3（默认 10s）自动续期。
//   - 把锁过期时间重置为 ttl，适配视频处理等长耗时场景
//   - 持有者宕机则 WatchDog 随之退出，锁会在 ttl 后自动过期，不会死锁
func (l *RedisLock) watchdog(ctx context.Context) {
	ticker := time.NewTicker(l.ttl / 3) // 10s
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 续期：Lua 脚本确保只有锁持有者才能续期
			l.renew(ctx)
		case <-l.stopChan:
			// 锁已释放，停止续期
			return
		case <-ctx.Done():
			return
		}
	}
}

// renew 续期锁（Lua 脚本保证原子性与持有者校验）。
// 返回 true 表示续期成功；false 表示锁已不归当前持有者或 Redis 异常——
// 续期失败会记录日志便于排查，WatchDog 不会因此退出（瞬时故障下次仍可续上），
// 真正的并发安全由业务层的幂等校验与状态机兜底。
func (l *RedisLock) renew(ctx context.Context) bool {
	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("expire", KEYS[1], ARGV[2])
	else
		return 0
	end`
	res, err := l.client.Eval(ctx, script, []string{l.key}, l.value, int(l.ttl.Seconds())).Result()
	if err != nil {
		log.Printf("[redis_lock] 续期失败 key=%s owner=%s err=%v", l.key, l.value, err)
		return false
	}
	n, _ := res.(int64)
	if n == 0 {
		// 锁已不归当前持有者（可能已过期被别人抢走），记录便于排查
		log.Printf("[redis_lock] 续期未生效：锁已不归当前持有者 key=%s owner=%s", l.key, l.value)
	}
	return n != 0
}

// Unlock 安全释放锁。
//  1. 先停止 WatchDog（stopChan 置 nil，防止重复 Unlock 时 double-close panic）
//  2. Lua 脚本：只有持有者才能删除，避免误删别人的锁
func (l *RedisLock) Unlock(ctx context.Context) error {
	// 停止 WatchDog
	if l.stopChan != nil {
		close(l.stopChan)
		l.stopChan = nil
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
