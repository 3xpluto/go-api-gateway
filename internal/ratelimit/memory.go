package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type memEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

type MemoryLimiter struct {
	mu      sync.Mutex
	m       map[string]*memEntry
	ttl     time.Duration
	cleanup time.Duration
	stopCh  chan struct{}
}

func NewMemoryLimiter(ttl time.Duration, cleanupEvery time.Duration) *MemoryLimiter {
	ml := &MemoryLimiter{
		m:       make(map[string]*memEntry),
		ttl:     ttl,
		cleanup: cleanupEvery,
		stopCh:  make(chan struct{}),
	}
	go ml.gcLoop()
	return ml
}

func (m *MemoryLimiter) gcLoop() {
	t := time.NewTicker(m.cleanup)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.mu.Lock()
			now := time.Now()
			for k, e := range m.m {
				if now.Sub(e.lastSeen) > m.ttl {
					delete(m.m, k)
				}
			}
			m.mu.Unlock()
		case <-m.stopCh:
			return
		}
	}
}

func (m *MemoryLimiter) Allow(ctx context.Context, key string, rps float64, burst float64, cost float64) (Decision, error) {
	m.mu.Lock()
	e := m.m[key]
	if e == nil {
		e = &memEntry{lim: rate.NewLimiter(rate.Limit(rps), int(burst))}
		m.m[key] = e
	}
	e.lastSeen = time.Now()
	lim := e.lim
	m.mu.Unlock()

	allowed := true
	for i := 0; i < int(cost); i++ {
		if !lim.Allow() {
			allowed = false
			break
		}
	}

	dec := Decision{Allowed: allowed, LimitRPS: rps, Burst: burst}
	if !allowed {
		dec.RetryAfterSeconds = 1
	}
	return dec, nil
}

func (m *MemoryLimiter) Close() error {
	close(m.stopCh)
	return nil
}
