package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
	"github.com/3xpluto/go-api-gatewayw/internal/ratelimit"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "./config/config.example.yaml", "path to yaml config")
	flag.Parse()

	log := logging.New()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Rate limiter backend
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

	// Build route table
	routes := make([]proxy.Route, 0, len(cfg.Routes))
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
			Proxy: proxy.BuildProxy(u),
		}
		routes = append(routes, r)
	}

	rtr, err := proxy.New(routes)
	if err != nil {
		log.Error("failed to create router", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Auth
	auth := mw.Authenticator{
		Mode:       cfg.Auth.Mode,
		HMACSecret: []byte(cfg.Auth.HMACSecret),
	}

	// Metrics
	reg := prometheus.NewRegistry()
	metrics := mw.NewMetrics(reg)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	ipr := mw.IPResolver{}

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := rtr.Match(r.URL.Path)
		if route == nil {
			http.NotFound(w, r)
			return
		}

		base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = proxy.StripPath(r.URL.Path, route.StripPrefix)
			route.Proxy.ServeHTTP(w, r)
		})

		h := base

		if route.AuthRequired {
			h = mw.RequireAuth(auth, h)
		}

		h = mw.RateLimit(limiter, ipr, mw.RateLimitConfig{
			Enabled:   route.RateLimit.Enabled,
			RPS:       route.RateLimit.RPS,
			Burst:     route.RateLimit.Burst,
			Scope:     route.RateLimit.Scope,
			RouteName: route.Name,
		}, h)

		// Outer -> inner:
		// RequestID -> WithRoute -> Instrument -> AccessLog -> (rate limit/auth/proxy)
		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, route.Name)
		h = mw.RequestID(h)

		h.ServeHTTP(w, r)
	}))

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("apigw listening", slog.String("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.String("error", err.Error()))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("shutdown complete")
}
