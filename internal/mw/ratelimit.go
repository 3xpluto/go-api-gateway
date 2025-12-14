package mw

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/yourname/apigw/internal/ratelimit"
)

type RateLimitConfig struct {
	Enabled   bool
	RPS       float64
	Burst     float64
	Scope     string // "user" | "ip"
	RouteName string
}

type IPResolver struct{}

func (IPResolver) ClientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func RateLimit(limiter ratelimit.Limiter, ipr IPResolver, cfg RateLimitConfig, next http.Handler) http.Handler {
	if !cfg.Enabled {
		return next
	}
	scope := strings.ToLower(cfg.Scope)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "rl:" + cfg.RouteName + ":"
		if scope == "user" {
			if sub, ok := Subject(r.Context()); ok {
				key += "u:" + sub
			} else {
				key += "ip:" + ipr.ClientIP(r)
			}
		} else {
			key += "ip:" + ipr.ClientIP(r)
		}

		dec, err := limiter.Allow(r.Context(), key, cfg.RPS, cfg.Burst, 1)
		if err != nil {
			// Fail-open in v1 to avoid a global outage if Redis is down.
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-RateLimit-Limit-RPS", trimFloat(cfg.RPS))
		w.Header().Set("X-RateLimit-Burst", trimFloat(cfg.Burst))

		if !dec.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(dec.RetryAfterSeconds))
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":               "rate_limited",
				"retry_after_seconds": dec.RetryAfterSeconds,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}
