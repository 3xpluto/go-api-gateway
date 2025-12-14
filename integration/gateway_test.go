package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/3xpluto/go-api-gateway/internal/mw"
	"github.com/3xpluto/go-api-gateway/internal/proxy"
	"github.com/3xpluto/go-api-gateway/internal/ratelimit"
)

type jwksAuth struct {
	v *mw.JWKSValidator
}

func (a jwksAuth) ValidateBearer(r *http.Request) (string, error) {
	authz := r.Header.Get("Authorization")
	if authz == "" || !strings.HasPrefix(authz, "Bearer ") {
		return "", io.EOF
	}
	tokStr := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	return a.v.Validate(r.Context(), tokStr)
}

func TestGateway_JWKS_Auth_And_RateLimit(t *testing.T) {
	// --- Upstreams
	usersUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "users",
			"path":    r.URL.Path,
		})
	}))
	defer usersUp.Close()

	publicUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "public",
			"path":    r.URL.Path,
		})
	}))
	defer publicUp.Close()

	// --- JWKS + RSA keypair
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "k1"
	issuer := "http://jwks.local"
	audience := "apigw"

	jwksJSON := map[string]any{
		"keys": []any{rsaPublicKeyToJWK(kid, &priv.PublicKey)},
	}

	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksJSON)
	}))
	defer jwksSrv.Close()

	validator, err := mw.NewJWKSValidator(jwksSrv.URL+"/.well-known/jwks.json", mw.JWKSValidatorOptions{
		HTTPTimeout: 2 * time.Second,
		CacheTTL:    5 * time.Minute,
		Leeway:      30 * time.Second,
		Issuers:     []string{issuer},
		Audiences:   []string{audience},
		ValidAlgs:   []string{"RS256"},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := jwksAuth{v: validator}

	// --- Build gateway handler (same pattern as cmd/gateway)
	log := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	limiter := ratelimit.NewMemoryLimiter(5*time.Minute, 200*time.Millisecond)
	defer limiter.Close()

	reg := prometheus.NewRegistry()
	metrics := mw.NewMetrics(reg)

	usersURL, _ := url.Parse(usersUp.URL)
	publicURL, _ := url.Parse(publicUp.URL)

	routes := []proxy.Route{
		{
			Name:         "users",
			PathPrefix:   "/api/users/",
			Upstream:     usersURL,
			StripPrefix:  "/api",
			AuthRequired: true,
			RateLimit: proxy.RouteRateLimit{
				Enabled: true,
				RPS:     5,
				Burst:   10,
				Scope:   "user",
			},
			Proxy: proxy.BuildProxy(usersURL, http.DefaultTransport),
		},
		{
			Name:       "public",
			PathPrefix: "/public/",
			Upstream:   publicURL,
			RateLimit: proxy.RouteRateLimit{
				Enabled: true,
				RPS:     1,
				Burst:   2,
				Scope:   "ip",
			},
			Proxy: proxy.BuildProxy(publicURL, http.DefaultTransport),
		},
	}

	rtr, err := proxy.New(routes)
	if err != nil {
		t.Fatal(err)
	}
	ipr := mw.IPResolver{}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := rtr.Match(r.URL.Path)
		if route == nil {
			http.NotFound(w, r)
			return
		}

		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = proxy.StripPath(r.URL.Path, route.StripPrefix)
			route.Proxy.ServeHTTP(w, r)
		})

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

		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, route.Name)
		h = mw.RequestID(h)

		h.ServeHTTP(w, r)
	}))

	gw := httptest.NewServer(mux)
	defer gw.Close()

	// --- Healthz should work with no upstreams/auth
	{
		resp, err := http.Get(gw.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200 healthz, got %d", resp.StatusCode)
		}
	}

	// --- Protected route: no token => 401
	{
		resp, err := http.Get(gw.URL + "/api/users/me")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, string(b))
		}
	}

	// --- Protected route: valid token => 200
	okToken := mintRS256Token(t, priv, kid, issuer, audience, "user_123")
	{
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, gw.URL+"/api/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+okToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, string(b))
		}
	}

	// --- Protected route: wrong audience => 401
	badAudToken := mintRS256Token(t, priv, kid, issuer, "WRONG", "user_123")
	{
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, gw.URL+"/api/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+badAudToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, string(b))
		}
	}

	// --- Rate limit public route: some requests should be 429
	{
		client := http.DefaultClient
		limited := 0
		ok := 0
		for i := 0; i < 12; i++ {
			resp, err := client.Get(gw.URL + "/public/hello")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == 429 {
				limited++
			} else if resp.StatusCode == 200 {
				ok++
			}
		}
		if limited == 0 {
			t.Fatalf("expected some 429s, got ok=%d limited=%d", ok, limited)
		}
	}
}

func TestGateway_ConcurrencyLimit_TooBusy(t *testing.T) {
	// Slow upstream so requests overlap in-flight.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer up.Close()

	log := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	reg := prometheus.NewRegistry()
	metrics := mw.NewMetrics(reg)

	upURL, _ := url.Parse(up.URL)

	routes := []proxy.Route{
		{
			Name:       "conc",
			PathPrefix: "/conc/",
			Upstream:   upURL,
			RateLimit: proxy.RouteRateLimit{
				Enabled: false, // do not interfere
			},
			Proxy: proxy.BuildProxy(upURL, http.DefaultTransport),
		},
	}

	rtr, err := proxy.New(routes)
	if err != nil {
		t.Fatal(err)
	}
	ipr := mw.IPResolver{}
	limiter := ratelimit.NewMemoryLimiter(5*time.Minute, 200*time.Millisecond)
	defer limiter.Close()

	sem := mw.NewSemaphore(1) // max 1 in-flight

	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := rtr.Match(r.URL.Path)
		if route == nil {
			http.NotFound(w, r)
			return
		}

		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route.Proxy.ServeHTTP(w, r)
		})

		// Concurrency limit should run BEFORE proxying.
		h = mw.ConcurrencyLimit(sem, h)

		// Keep rate limiter in the chain but disabled.
		h = mw.RateLimit(limiter, ipr, mw.RateLimitConfig{
			Enabled:   false,
			RouteName: route.Name,
		}, h)

		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, route.Name)
		h = mw.RequestID(h)

		h.ServeHTTP(w, r)
	}))

	gw := httptest.NewServer(mux)
	defer gw.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// Fire a burst of truly parallel requests.
	const n = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)

	var okCount int32
	var busyCount int32
	var busySawBody int32

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start

			resp, err := client.Get(gw.URL + "/conc/hello")
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				atomic.AddInt32(&okCount, 1)
				return
			}
			if resp.StatusCode == 503 {
				atomic.AddInt32(&busyCount, 1)
				b, _ := io.ReadAll(resp.Body)
				if strings.Contains(string(b), `"error":"too_busy"`) {
					atomic.AddInt32(&busySawBody, 1)
				}
				return
			}
		}()
	}

	close(start)
	wg.Wait()

	if okCount == 0 {
		t.Fatalf("expected at least one 200, got ok=%d busy=%d", okCount, busyCount)
	}
	if busyCount == 0 {
		t.Fatalf("expected at least one 503 too_busy, got ok=%d busy=%d", okCount, busyCount)
	}
	if busySawBody == 0 {
		t.Fatalf("expected at least one 503 body to contain error=too_busy")
	}
}

// NEW: Circuit breaker integration test (opens on 5xx then closes on success)
func TestGateway_CircuitBreaker_Opens_And_Closes(t *testing.T) {
	var calls int32

	// Upstream fails twice (500), then succeeds (200).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer up.Close()

	log := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reg := prometheus.NewRegistry()
	metrics := mw.NewMetrics(reg)

	upURL, _ := url.Parse(up.URL)

	routes := []proxy.Route{
		{
			Name:       "cb",
			PathPrefix: "/cb/",
			Upstream:   upURL,
			RateLimit: proxy.RouteRateLimit{
				Enabled: false,
			},
			Proxy: proxy.BuildProxy(upURL, http.DefaultTransport),
		},
	}

	rtr, err := proxy.New(routes)
	if err != nil {
		t.Fatal(err)
	}
	ipr := mw.IPResolver{}
	limiter := ratelimit.NewMemoryLimiter(5*time.Minute, 200*time.Millisecond)
	defer limiter.Close()

	br := mw.NewCircuitBreaker(mw.BreakerConfig{
		Enabled:             true,
		FailureThreshold:    2,
		OpenDuration:        200 * time.Millisecond,
		HalfOpenMaxInFlight: 1,
	})

	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := rtr.Match(r.URL.Path)
		if route == nil {
			http.NotFound(w, r)
			return
		}

		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route.Proxy.ServeHTTP(w, r)
		})

		// Breaker should see upstream status codes (500s).
		h = mw.CircuitBreak(br, h)

		h = mw.RateLimit(limiter, ipr, mw.RateLimitConfig{
			Enabled:   false,
			RouteName: route.Name,
		}, h)

		h = mw.AccessLog(log, h)
		h = mw.Instrument(metrics, h)
		h = mw.WithRoute(h, route.Name)
		h = mw.RequestID(h)

		h.ServeHTTP(w, r)
	}))

	gw := httptest.NewServer(mux)
	defer gw.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1) first request hits upstream and returns 500
	{
		resp, err := client.Get(gw.URL + "/cb/hello")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 500 {
			t.Fatalf("expected 500 on first call, got %d", resp.StatusCode)
		}
	}

	// 2) second request hits upstream and returns 500 => breaker opens
	{
		resp, err := client.Get(gw.URL + "/cb/hello")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 500 {
			t.Fatalf("expected 500 on second call, got %d", resp.StatusCode)
		}
	}

	// 3) third request should be fast-failed by breaker: 503 + circuit_open body
	{
		resp, err := client.Get(gw.URL + "/cb/hello")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 503 {
			t.Fatalf("expected 503 after breaker opens, got %d body=%s", resp.StatusCode, string(b))
		}
		if !strings.Contains(string(b), `"error":"circuit_open"`) {
			t.Fatalf("expected circuit_open body, got body=%s", string(b))
		}
		if br.Stats().State != mw.BreakerOpen {
			t.Fatalf("expected breaker state open, got %s", br.Stats().State)
		}
	}

	// Wait for open duration to elapse so next request is half-open.
	time.Sleep(250 * time.Millisecond)

	// 4) upstream now succeeds => breaker should close
	{
		resp, err := client.Get(gw.URL + "/cb/hello")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("expected 200 after open window, got %d body=%s", resp.StatusCode, string(b))
		}
		if br.Stats().State != mw.BreakerClosed {
			t.Fatalf("expected breaker state closed after success, got %s", br.Stats().State)
		}
	}

	// 5) subsequent calls should stay 200
	{
		resp, err := client.Get(gw.URL + "/cb/hello")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200 after breaker closed, got %d", resp.StatusCode)
		}
	}
}

func mintRS256Token(t *testing.T, priv *rsa.PrivateKey, kid string, iss string, aud string, sub string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": iss,
		"aud": aud,
		"sub": sub,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rsaPublicKeyToJWK(kid string, pub *rsa.PublicKey) map[string]any {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	return map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": kid,
		"n":   n,
		"e":   e,
	}
}
