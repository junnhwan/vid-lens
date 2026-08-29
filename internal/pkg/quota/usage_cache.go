package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"vid-lens/internal/model"
)

var applyUsageScript = redis.NewScript(`
local marker=KEYS[1]
if redis.call('EXISTS',marker)==1 then return 0 end
redis.call('HINCRBYFLOAT',KEYS[2],ARGV[1],ARGV[2])
redis.call('SET',marker,'1','EX',ARGV[3])
redis.call('EXPIRE',KEYS[2],ARGV[3])
return 1
`)

type RedisUsageCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

func NewRedisUsageCache(client redis.Cmdable, ttl time.Duration) *RedisUsageCache {
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return &RedisUsageCache{client: client, ttl: ttl}
}
func (c *RedisUsageCache) Apply(ctx context.Context, e model.QuotaCompensation) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis usage cache is not initialized")
	}
	if e.EventKey == "" || e.UserID <= 0 || e.UsageDate == "" || e.Kind == "" || e.Unit == "" {
		return fmt.Errorf("invalid quota compensation event")
	}
	marker := "quota:usage-event:" + e.EventKey
	daily := fmt.Sprintf("quota:daily:%d:%s", e.UserID, e.UsageDate)
	field := e.Kind + ":" + e.Unit
	return applyUsageScript.Run(ctx, c.client, []string{marker, daily}, field, e.DeltaUnits, int64(c.ttl/time.Second)).Err()
}
