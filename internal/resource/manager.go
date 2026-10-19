package resource

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type Manager struct {
	Store        *store.Store
	Redis        *redis.Client
	Config       config.Config
	memoryMu     sync.Mutex
	paused       map[string]bool
	recoverSince map[string]time.Time
}

func New(s *store.Store, c config.Config) (*Manager, error) {
	o, e := redis.ParseURL(c.RedisURL)
	if e != nil {
		return nil, errors.New("invalid Redis URL")
	}
	o.DialTimeout = 10 * time.Second
	o.ReadTimeout = 5 * time.Second
	o.WriteTimeout = 5 * time.Second
	return &Manager{Store: s, Redis: redis.NewClient(o), Config: c}, nil
}
func (m *Manager) Acquire(ctx context.Context, c domain.Claim, name string) (context.Context, func(), error) {
	if e := m.Redis.Ping(ctx).Err(); e != nil {
		return nil, nil, domain.Err(503, "SERVICE_UNAVAILABLE", "资源调度暂不可用。")
	}
	if e := m.waitMemory(ctx, name); e != nil {
		return nil, nil, e
	}
	if name == "ai" {
		if c.Run.Kind == "qa.answer" {
			name = "ai:interactive"
		} else {
			name = "ai:generation"
		}
	}
	owner := domain.ID()
	baseName, slots := name, 1
	if name == "asr" {
		slots = 2
	}
	redisKey := ""
acquire:
	for {
		if baseName == "ai:generation" {
			v, e := m.Store.ExecutionSettings(ctx)
			if e != nil {
				return nil, nil, e
			}
			slots = v.MaxVideoRunning
		}
		for slot := 0; slot < slots; slot++ {
			name = baseName
			if slots > 1 || baseName == "ai:generation" {
				name = fmt.Sprintf("%s:%d", baseName, slot)
			}
			redisKey = "vidlens:resource:" + name
			fast, e := m.Redis.SetNX(ctx, redisKey, owner, 60*time.Second).Result()
			if e != nil {
				return nil, nil, domain.Err(503, "SERVICE_UNAVAILABLE", "资源调度暂不可用。")
			}
			if !fast {
				continue
			}
			ok, e := m.Store.AcquireResource(ctx, c, name, owner, 1)
			if e == nil && ok {
				break acquire
			}
			_, _ = m.Redis.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{redisKey}, owner).Result()
			if e != nil {
				return nil, nil, e
			}
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-t.C:
				renew, err := m.Redis.Eval(child, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('EXPIRE',KEYS[1],60) end return 0`, []string{redisKey}, owner).Int64()
				if err != nil || renew != 1 || m.Store.RenewResource(child, c, name, owner) != nil {
					cancel()
					return
				}
			}
		}
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			cctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
			defer cc()
			_ = m.Store.ReleaseResource(cctx, name, owner)
			_, _ = m.Redis.Eval(cctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{redisKey}, owner).Result()
		})
	}
	return child, release, nil
}
func (m *Manager) waitMemory(ctx context.Context, name string) error {
	lane := "ai"
	threshold := int64(256 * 1024)
	if name == "ffmpeg" || strings.HasPrefix(name, "download") {
		lane = "media"
		threshold = 512 * 1024
	}
	for {
		kb := availableMemory()
		if kb == 0 {
			return nil
		}
		m.memoryMu.Lock()
		if m.paused == nil {
			m.paused = map[string]bool{}
			m.recoverSince = map[string]time.Time{}
		}
		if kb < threshold {
			m.paused[lane] = true
			m.recoverSince[lane] = time.Time{}
		}
		ready := !m.paused[lane]
		if !ready {
			if kb >= 768*1024 {
				if m.recoverSince[lane].IsZero() {
					m.recoverSince[lane] = time.Now()
				}
				if time.Since(m.recoverSince[lane]) >= 60*time.Second {
					delete(m.paused, lane)
					ready = true
				}
			} else {
				m.recoverSince[lane] = time.Time{}
			}
		}
		m.memoryMu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
func availableMemory() int64 {
	b, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				n, _ := strconv.ParseInt(fields[1], 10, 64)
				return n
			}
		}
	}
	return 0
}
func memoryCheck(resource string) error {
	b, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return nil
	}
	var kb int64
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			f := strings.Fields(line)
			kb, _ = strconv.ParseInt(f[1], 10, 64)
		}
	}
	threshold := int64(256 * 1024)
	if resource == "ffmpeg" || resource == "download" {
		threshold = 512 * 1024
	}
	if kb > 0 && kb < threshold {
		return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "当前可用内存不足。")
	}
	return nil
}
func (m *Manager) Reserve(ctx context.Context, c domain.Claim, temp, images int64) error {
	var st syscall.Statfs_t
	if e := syscall.Statfs(m.Config.DataDir, &st); e != nil {
		return e
	}
	return m.Store.ReserveStorage(ctx, c, temp, images, int64(st.Bavail)*int64(st.Bsize), int64(st.Blocks)*int64(st.Bsize))
}

type cacheValue struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func cacheSlot(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("vidlens:cache:%d", binary.BigEndian.Uint16(h[:2])%512)
}
func (m *Manager) Cached(ctx context.Context, key string, out any) bool {
	b, e := m.Redis.Get(ctx, cacheSlot(key)).Bytes()
	if e != nil {
		return false
	}
	var c cacheValue
	if json.Unmarshal(b, &c) != nil || c.Key != key {
		return false
	}
	return json.Unmarshal(c.Value, out) == nil
}
func (m *Manager) Cache(ctx context.Context, key string, value any) {
	b := domain.JSON(cacheValue{Key: key, Value: domain.JSON(value)})
	if len(b) <= 64<<10 {
		_ = m.Redis.Set(ctx, cacheSlot(key), b, 10*time.Minute).Err()
	}
}
func (m *Manager) Limit(ctx context.Context, scope string, limit int) bool {
	h := sha256.Sum256([]byte(scope))
	k := "vidlens:rate:" + hex.EncodeToString(h[:16])
	v, e := m.Redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; return n`, []string{k}).Int64()
	return e != nil || v <= int64(limit)
}
