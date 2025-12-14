package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/3xpluto/go-api-gateway/internal/config"
	"github.com/3xpluto/go-api-gateway/internal/logging"
	"github.com/3xpluto/go-api-gateway/internal/mw"
	"github.com/3xpluto/go-api-gateway/internal/proxy"
	"github.com/3xpluto/go-api-gateway/internal/ratelimit"
)

type jwksAuthAdapter struct {
	v *mw.JWKSValidator
}

func (a jwksAuthAdapter) ValidateBearer(r *http.Request) (string, error) {
	authz := r.Header.Get("Authorization")
	if authz == "" || !strings.HasPrefix(authz, "Bearer ") {
		return "", errors.New("missing bearer token")
	}
	tokStr := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	return a.v.Validate(r.Context(), tokStr)
}

func main() {
	var configPath string
	var validateOnly bool
	flag.StringVar(&configPath, "config", "./config/config.example.yaml", "path to yaml config")
	flag.BoolVar(&validateOnly, "validate-config", false, "validate config and exit")
	flag.Parse()

	log := logging.New()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := validateConfig(cfg); err != nil {
		log.Error("config validation failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if validateOnly {
		log.Info("config ok")
		return
	}

	// ---- Rate limiter backend
	var limiter ratelimit.Limiter
	backend := strings.ToLower(cfg.RateLimit.Backend)

	switch backend {
	case "redis":
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.RateLimit.Redis.Addr,
			Password: cfg.RateLimit.Redis.Password,
			DB:       cfg.RateLimit.Redis.DB,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Warn("redis unreachable; falling back to memory limiter", slog.String("error", err.Error()))
			limiter = ratelimit.NewMemoryLimiter(5*time.Minute, time.Minute)
		} else {
			limiter = ratelimit.NewRedisLimiter(rdb)
		}

	case "memory":
		limiter = ratelimit.NewMemoryLimiter(
			time.Duration(cfg.RateLimit.Memory.TTLSeconds)*time.Second,
			time.Duration(cfg.RateLimit.Memory.CleanupSeconds)*time.Second,
		)

	default:
		log.Error("unknown rate_limit.backend", slog.String("backend", cfg.RateLimit.Backend))
		os.Exit(1)
	}
	defer limiter.Close()

	// ---- Transport for upstream calls (hardened defaults)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	// ---- Auth handler (HS256 or JWKS)
	var authHandler mw.AuthHandler
	var jwksValidator *mw.JWKSValidator

	switch strings.ToLower(cfg.Auth.Mode) {
	case "jwks":
		// Assumes your config has cfg.Auth.JWKS.{URL,CacheTTLSeconds,HTTPTimeoutSeconds,LeewaySeconds,Issuers,Audiences}
		v, err := mw.NewJWKSValidator(cfg.Auth.JWKS.URL, mw.JWKSValidatorOptions{
			HTTPTimeout: time.Duration(cfg.Auth.JWKS.HTTPTimeoutSeconds) * time.Second,
			CacheTTL:    time.Duration(cfg.Auth.JWKS.CacheTTLSeconds) * time.Second,
			Leeway:      time.Duration(cfg.Auth.JWKS.LeewaySeconds) * time.Second,
			Issuers:     cfg.Auth.JWKS.Issuers,
			Audiences:   cfg.Auth.JWKS.Audiences,
			ValidAlgs:   []string{"RS256"},
		})
		if err != nil {
			log.Error("failed to init jwks validator", slog.String("error", err.Error()))
			os.Exit(1)
		}
		jwksValidator = v
		authHandler = jwksAuthAdapter{v: v}

	case "hmac", "":
		a := mw.Authenticator{
			Mode:       "hmac",
			HMACSecret: []byte(cfg.Auth.HMACSecret),
		}
		authHandler = a

	default:
		log.Error("unknown auth.mode", slog.String("mode", cfg.Auth.Mode))
		os.Exit(1)
	}

	// ---- Build route table + per-route semaphores/breakers
	routes := make([]proxy.Route, 0, len(cfg.Routes))
	sems := map[string]*mw.Semaphore{}
	breakers := map[string]*mw.CircuitBreaker{}

	for _, rc := range cfg.Routes {
		u, err := url.Parse(rc.Upstream)
		if err != nil {
			log.Error("invalid upstream url", slog.String("route", rc.Name), slog.String("error", err.Error()))
			os.Exit(1)
		}

		r := proxy.Route{
			Name:         rc.Name,
			PathPrefix:   rc.Match.PathPrefix,
			Upstream:     u,
			StripPrefix:  rc.StripPrefix,
			AuthRequired: rc.AuthRequired,
			RateLimit: proxy.RouteRateLimit{
				Enabled: rc.RateLimit.Enabled,
				RPS:     rc.RateLimit.RPS,
				Burst:   rc.RateLimit.Burst,
				Scope:   rc.RateLimit.Scope,
			},
			Proxy: proxy.BuildProxy(u, transport),
		}
		routes = append(routes, r)

		// Concurrency per route
		sems[rc.Name] = mw.NewSemaphore(rc.Concurrency.MaxInFlight)

		// Circuit breaker per route
		breakers[rc.Name] = mw.NewCircuitBreaker(mw.BreakerConfig{
			Enabled:             rc.CircuitBreaker.Enabled,
			FailureThreshold:    rc.CircuitBreaker.FailureThreshold,
			OpenDuration:        time.Duration(rc.CircuitBreaker.OpenSeconds) * time.Second,
			HalfOpenMaxInFlight: rc.CircuitBreaker.HalfOpenMaxInFlight,
		})
	}

	rtr, err := proxy.New(routes)
	if err != nil {
		log.Error("failed to create router", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ---- Metrics
	reg := prometheus.NewRegistry()
	metrics := mw.NewMetrics(reg)

	// ---- HTTP server / mux
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok")); err != nil {
			return
		}
	})

	ipr := mw.IPResolver{}
	startedAt := time.Now()
	adminKey := os.Getenv("APIGW_ADMIN_KEY")

	// ---- Admin endpoints (guarded)
	wrapAdmin := func(routeName string, h http.Handler) http.Handler {
		h = mw.RequireAdminKey(adminKey, h)
		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, routeName)
		h = mw.RequestID(h)
		return h
	}

	mux.Handle("/-/status", wrapAdmin("admin_status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		info, _ := debug.ReadBuildInfo()
		goVer := ""
		if info != nil {
			goVer = info.GoVersion
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time_utc":          time.Now().UTC().Format(time.RFC3339),
			"uptime_seconds":    int(time.Since(startedAt).Seconds()),
			"listen_addr":       cfg.Server.Addr,
			"go_version":        goVer,
			"auth_mode":         cfg.Auth.Mode,
			"rate_backend":      cfg.RateLimit.Backend,
			"routes_configured": len(cfg.Routes),
		})
	})))

	mux.Handle("/-/routes", wrapAdmin("admin_routes", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		type outRoute struct {
			Name           string `json:"name"`
			PathPrefix     string `json:"path_prefix"`
			Upstream       string `json:"upstream"`
			StripPrefix    string `json:"strip_prefix"`
			AuthRequired   bool   `json:"auth_required"`
			RateLimit      any    `json:"rate_limit"`
			Concurrency    any    `json:"concurrency"`
			CircuitBreaker any    `json:"circuit_breaker"`
		}

		out := make([]outRoute, 0, len(cfg.Routes))
		for _, rc := range cfg.Routes {
			out = append(out, outRoute{
				Name:         rc.Name,
				PathPrefix:   rc.Match.PathPrefix,
				Upstream:     rc.Upstream,
				StripPrefix:  rc.StripPrefix,
				AuthRequired: rc.AuthRequired,
				RateLimit: map[string]any{
					"enabled": rc.RateLimit.Enabled,
					"rps":     rc.RateLimit.RPS,
					"burst":   rc.RateLimit.Burst,
					"scope":   rc.RateLimit.Scope,
				},
				Concurrency: map[string]any{
					"max_in_flight": rc.Concurrency.MaxInFlight,
				},
				CircuitBreaker: map[string]any{
					"enabled":                 rc.CircuitBreaker.Enabled,
					"failure_threshold":       rc.CircuitBreaker.FailureThreshold,
					"open_seconds":            rc.CircuitBreaker.OpenSeconds,
					"half_open_max_in_flight": rc.CircuitBreaker.HalfOpenMaxInFlight,
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})))

	mux.Handle("/-/auth", wrapAdmin("admin_auth", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out := map[string]any{
			"mode": cfg.Auth.Mode,
		}
		if jwksValidator != nil {
			out["jwks"] = jwksValidator.Stats()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})))

	mux.Handle("/-/limits", wrapAdmin("admin_limits", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rows := make([]map[string]any, 0, len(cfg.Routes))
		for _, rc := range cfg.Routes {
			row := map[string]any{"route": rc.Name}

			if sem := sems[rc.Name]; sem != nil && sem.Enabled() {
				row["concurrency"] = map[string]any{
					"max_in_flight": sem.Cap(),
					"in_flight":     sem.InUse(),
				}
			}
			if br := breakers[rc.Name]; br != nil {
				row["circuit_breaker"] = br.Stats()
			}
			rows = append(rows, row)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	})))

	// ---- Main gateway handler (catch-all)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := rtr.Match(r.URL.Path)
		if route == nil {
			http.NotFound(w, r)
			return
		}

		// Base proxy handler
		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = proxy.StripPath(r.URL.Path, route.StripPrefix)
			route.Proxy.ServeHTTP(w, r)
		})

		// Circuit breaker should see upstream status codes.
		if br := breakers[route.Name]; br != nil {
			h = mw.CircuitBreak(br, h)
		}

		// Concurrency should NOT count as breaker failure; keep it outside breaker.
		if sem := sems[route.Name]; sem != nil && sem.Enabled() {
			h = mw.ConcurrencyLimit(sem, h)
		}

		// Auth + RL should run outside breaker/concurrency so 401/429 don't affect breaker.
		if route.AuthRequired {
			h = mw.RequireAuth(authHandler, h)
		}

		h = mw.RateLimit(limiter, ipr, mw.RateLimitConfig{
			Enabled:   route.RateLimit.Enabled,
			RPS:       route.RateLimit.RPS,
			Burst:     route.RateLimit.Burst,
			Scope:     route.RateLimit.Scope,
			RouteName: route.Name,
		}, h)

		// Cross-cutting middleware (outermost -> innermost)
		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, route.Name)
		h = mw.RequestID(h)

		h.ServeHTTP(w, r)
	}))

	// ---- Server
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	go func() {
		log.Info("apigw listening", slog.String("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.String("error", err.Error()))
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("shutdown complete")
}

func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if cfg.Server.Addr == "" {
		return errors.New("server.addr is required")
	}
	if len(cfg.Routes) == 0 {
		return errors.New("at least one route is required")
	}

	seenNames := map[string]struct{}{}
	for _, r := range cfg.Routes {
		if r.Name == "" {
			return errors.New("route.name is required")
		}
		if _, ok := seenNames[r.Name]; ok {
			return errors.New("duplicate route.name: " + r.Name)
		}
		seenNames[r.Name] = struct{}{}

		if r.Match.PathPrefix == "" || !strings.HasPrefix(r.Match.PathPrefix, "/") {
			return errors.New("route.match.path_prefix must start with / for route: " + r.Name)
		}
		if r.Upstream == "" {
			return errors.New("route.upstream is required for route: " + r.Name)
		}
		if _, err := url.Parse(r.Upstream); err != nil {
			return errors.New("invalid route.upstream for route: " + r.Name)
		}

		if r.RateLimit.Enabled {
			if r.RateLimit.RPS <= 0 || r.RateLimit.Burst <= 0 {
				return errors.New("rate_limit rps/burst must be > 0 for route: " + r.Name)
			}
			scope := strings.ToLower(r.RateLimit.Scope)
			if scope != "ip" && scope != "user" && scope != "" {
				return errors.New("rate_limit.scope must be ip or user for route: " + r.Name)
			}
		}

		if r.Concurrency.MaxInFlight < 0 {
			return errors.New("concurrency.max_in_flight cannot be negative for route: " + r.Name)
		}

		if r.CircuitBreaker.Enabled {
			if r.CircuitBreaker.FailureThreshold <= 0 {
				return errors.New("circuit_breaker.failure_threshold must be > 0 for route: " + r.Name)
			}
			if r.CircuitBreaker.OpenSeconds <= 0 {
				return errors.New("circuit_breaker.open_seconds must be > 0 for route: " + r.Name)
			}
			if r.CircuitBreaker.HalfOpenMaxInFlight <= 0 {
				return errors.New("circuit_breaker.half_open_max_in_flight must be > 0 for route: " + r.Name)
			}
		}
	}
	return nil
}
