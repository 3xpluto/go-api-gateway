package mw

import (
	"encoding/json"
	"net/http"
)

// Semaphore is a tiny counting semaphore for per-route in-flight limiting.
type Semaphore struct {
	ch chan struct{}
}

func NewSemaphore(maxInFlight int) *Semaphore {
	if maxInFlight <= 0 {
		return &Semaphore{ch: nil}
	}
	return &Semaphore{ch: make(chan struct{}, maxInFlight)}
}

func (s *Semaphore) Enabled() bool { return s != nil && s.ch != nil }
func (s *Semaphore) Cap() int {
	if s == nil || s.ch == nil {
		return 0
	}
	return cap(s.ch)
}
func (s *Semaphore) InUse() int {
	if s == nil || s.ch == nil {
		return 0
	}
	return len(s.ch)
}

func (s *Semaphore) TryAcquire() bool {
	if s == nil || s.ch == nil {
		return true
	}
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Semaphore) Release() {
	if s == nil || s.ch == nil {
		return
	}
	select {
	case <-s.ch:
	default:
	}
}

// ConcurrencyLimit rejects requests when too many are already in-flight for a route.
func ConcurrencyLimit(sem *Semaphore, next http.Handler) http.Handler {
	if sem == nil || !sem.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sem.TryAcquire() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":         "too_busy",
				"message":       "route is at max concurrency",
				"route":         RouteName(r.Context()),
				"max_in_flight": sem.Cap(),
			})
			return
		}
		defer sem.Release()
		next.ServeHTTP(w, r)
	})
}
