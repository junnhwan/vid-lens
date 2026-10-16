package resource

import (
	"context"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

// Operational probes share the actual generation AI slot. A probe is never a
// fake video job and retains its own epoch and physical-submission budget.
func (m *Manager) AcquireProbe(ctx context.Context, p store.Probe) (context.Context, func(), error) {
	if e := m.Redis.Ping(ctx).Err(); e != nil {
		return nil, nil, e
	}
	if e := m.waitMemory(ctx, "ai"); e != nil {
		return nil, nil, e
	}
	const name = "ai:generation"
	const key = "vidlens:resource:" + name
	owner := domain.ID()
	unlock := func() {
		c, cc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cc()
		_ = m.Store.ReleaseResource(c, name, owner)
		_, _ = m.Redis.Eval(c, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, owner).Result()
	}
	for {
		fast, e := m.Redis.SetNX(ctx, key, owner, 60*time.Second).Result()
		if e != nil {
			return nil, nil, e
		}
		if fast {
			ok, e := m.Store.AcquireProbeResource(ctx, p, name, owner)
			if e != nil {
				unlock()
				return nil, nil, e
			}
			if ok {
				break
			}
			unlock()
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-tick.C:
				n, e := m.Redis.Eval(child, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('EXPIRE',KEYS[1],60) end return 0`, []string{key}, owner).Int64()
				if e != nil || n != 1 || m.Store.RenewProbeResource(child, p, name, owner) != nil {
					cancel()
					return
				}
			}
		}
	}()
	return child, func() { cancel(); <-done; unlock() }, nil
}
