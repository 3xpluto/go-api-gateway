package mw

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/3xpluto/go-api-gateway/internal/httpx"
)

type BreakerState string

const (
	BreakerClosed   BreakerState = "closed"
	BreakerOpen     BreakerState = "open"
	BreakerHalfOpen BreakerState = "half_open"
)

type BreakerConfig struct {
	Enabled             bool
	FailureThreshold    int           // consecutive failures to open
	OpenDuration        time.Duration // how long to stay open
	HalfOpenMaxInFlight int           // how many trial requests in half-open
}

type CircuitBreaker struct {
	cfg BreakerConfig

	mu sync.Mutex

	state BreakerState
	fails int

	opensAt time.Time

	// half-open throttling
	halfInFlight int
}

func NewCircuitBreaker(cfg BreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 10 * time.Second
	}
	if cfg.HalfOpenMaxInFlight <= 0 {
		cfg.HalfOpenMaxInFlight = 1
	}
	return &CircuitBreaker{
		cfg:   cfg,
		state: BreakerClosed,
	}
}

type BreakerStats struct {
	State         BreakerState `json:"state"`
	Failures      int          `json:"failures"`
	OpensAt       time.Time    `json:"opens_at"`
	RetryAfterSec int          `json:"retry_after_seconds"`
	HalfInFlight  int          `json:"half_open_in_flight"`
}

func (b *CircuitBreaker) Stats() BreakerStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	retry := 0
	if b.state == BreakerOpen {
		rem := b.cfg.OpenDuration - time.Since(b.opensAt)
		if rem > 0 {
			retry = int((rem + 999*time.Millisecond) / time.Second)
		}
	}
	return BreakerStats{
		State:         b.state,
		Failures:      b.fails,
		OpensAt:       b.opensAt,
		RetryAfterSec: retry,
		HalfInFlight:  b.halfInFlight,
	}
}

func (b *CircuitBreaker) allowLocked(now time.Time) (allowed bool, retryAfter time.Duration) {
	if !b.cfg.Enabled {
		return true, 0
	}

	switch b.state {
	case BreakerClosed:
		return true, 0

	case BreakerOpen:
		// if open window elapsed, transition to half-open
		if now.Sub(b.opensAt) >= b.cfg.OpenDuration {
			b.state = BreakerHalfOpen
			b.fails = 0
			b.halfInFlight = 0
			return b.allowLocked(now)
		}
		rem := b.cfg.OpenDuration - now.Sub(b.opensAt)
		if rem < 0 {
			rem = 0
		}
		return false, rem

	case BreakerHalfOpen:
		if b.halfInFlight >= b.cfg.HalfOpenMaxInFlight {
			return false, 1 * time.Second
		}
		b.halfInFlight++
		return true, 0

	default:
		return true, 0
	}
}

func (b *CircuitBreaker) doneLocked(success bool) {
	if !b.cfg.Enabled {
		return
	}
	switch b.state {
	case BreakerClosed:
		if success {
			b.fails = 0
			return
		}
		b.fails++
		if b.fails >= b.cfg.FailureThreshold {
			b.state = BreakerOpen
			b.opensAt = time.Now()
		}

	case BreakerHalfOpen:
		if b.halfInFlight > 0 {
			b.halfInFlight--
		}
		if success {
			// one success closes the breaker (simple + safe default)
			b.state = BreakerClosed
			b.fails = 0
			return
		}
		// failed trial => reopen
		b.state = BreakerOpen
		b.opensAt = time.Now()
		b.fails = b.cfg.FailureThreshold

	case BreakerOpen:
		// nothing to do
	}
}

// CircuitBreak rejects requests when the breaker is open.
// It counts failures when downstream returns >= 500.
func CircuitBreak(b *CircuitBreaker, next http.Handler) http.Handler {
	if b == nil || !b.cfg.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		b.mu.Lock()
		allowed, retry := b.allowLocked(now)
		b.mu.Unlock()

		if !allowed {
			if retry > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int((retry+999*time.Millisecond)/time.Second)))
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   "circuit_open",
				"message": "upstream temporarily unavailable",
				"route":   RouteName(r.Context()),
			})
			return
		}

		sw := &httpx.StatusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		// Consider 5xx as failure; 4xx is not an upstream health failure.
		status := sw.Status
		if status == 0 {
			status = http.StatusOK
		}
		success := status < 500

		b.mu.Lock()
		b.doneLocked(success)
		b.mu.Unlock()
	})
}
