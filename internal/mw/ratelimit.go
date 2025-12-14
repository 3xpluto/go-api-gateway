package mw

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/3xpluto/go-api-gateway/internal/netx"
	"github.com/3xpluto/go-api-gateway/internal/ratelimit"
)

type RateLimitConfig struct {
	Enabled   bool
	RPS       float64
	Burst     float64
	Scope     string // "user" | "ip"
	RouteName string
}

type IPResolver struct {
	Trusted *netx.CIDRSet
}

func (r IPResolver) ClientIP(req *http.Request) string {
	remoteIP := parseRemoteIP(req.RemoteAddr)
	if remoteIP != nil && r.Trusted != nil && r.Trusted.Contains(remoteIP) {
		// Only trust forwarded headers from trusted proxies
		if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
			// first IP is original client (left-most)
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				ip := net.ParseIP(strings.TrimSpace(parts[0]))
				if ip != nil {
					return ip.String()
				}
			}
		}
		if xrip := net.ParseIP(strings.TrimSpace(req.Header.Get("X-Real-Ip"))); xrip != nil {
			return xrip.String()
		}
	}
	if remoteIP != nil {
		return remoteIP.String()
	}
	return req.RemoteAddr
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return net.ParseIP(remoteAddr)
	}
	return net.ParseIP(host)
}

func RateLimit(limiter ratelimit.Limiter, ipr IPResolver, cfg RateLimitConfig, next http.Handler) http.Handler {
	if !cfg.Enabled {
		return next
	}
	scope := strings.ToLower(cfg.Scope)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "rl:" + cfg.RouteName + ":"
		actor := ""
		if scope == "user" {
			if sub, ok := Subject(r.Context()); ok {
				key += "u:" + sub
				actor = "user"
			} else {
				key += "ip:" + ipr.ClientIP(r)
				actor = "ip"
			}
		} else {
			key += "ip:" + ipr.ClientIP(r)
			actor = "ip"
		}

		dec, err := limiter.Allow(r.Context(), key, cfg.RPS, cfg.Burst, 1)
		if err != nil {
			// Fail-open in v1 to avoid a global outage if Redis is down.
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-RateLimit-Route", cfg.RouteName)
		w.Header().Set("X-RateLimit-Scope", actor)
		w.Header().Set("X-RateLimit-Limit-RPS", trimFloat(cfg.RPS))
		w.Header().Set("X-RateLimit-Burst", trimFloat(cfg.Burst))
		if dec.Remaining > 0 {
			w.Header().Set("X-RateLimit-Remaining", trimFloat(dec.Remaining))
		}

		if !dec.Allowed {
			retry := dec.RetryAfterSeconds
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(retry)*time.Second).Unix(), 10))
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":               "rate_limited",
				"route":               cfg.RouteName,
				"scope":               actor,
				"retry_after_seconds": retry,
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
