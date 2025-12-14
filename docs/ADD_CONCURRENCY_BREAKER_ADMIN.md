# Add per-route concurrency limits + circuit breaker + admin debug endpoints

This patch adds:
- `internal/mw/concurrency.go`
- `internal/mw/circuit_breaker.go`
- `internal/mw/admin_key.go`
- `internal/mw/jwks_stats.go`

You still need to wire them into `cmd/gateway/main.go` and your config structs.

---

## 1) Config YAML (example)

Add per-route config blocks:

```yaml
routes:
  - name: "users"
    match:
      path_prefix: "/api/users/"
    upstream: "http://127.0.0.1:9001"
    strip_prefix: "/api"
    auth_required: true

    rate_limit:
      enabled: true
      rps: 5
      burst: 10
      scope: "user"

    concurrency:
      max_in_flight: 50

    circuit_breaker:
      enabled: true
      failure_threshold: 5
      open_seconds: 10
      half_open_max_in_flight: 1
```

---

## 2) Config structs (add these fields)

Wherever your route config is defined (often `internal/config/config.go`), add:

```go
type RouteConcurrency struct {
    MaxInFlight int `yaml:"max_in_flight"`
}

type RouteCircuitBreaker struct {
    Enabled bool `yaml:"enabled"`
    FailureThreshold int `yaml:"failure_threshold"`
    OpenSeconds int `yaml:"open_seconds"`
    HalfOpenMaxInFlight int `yaml:"half_open_max_in_flight"`
}

type RouteConfig struct {
    // ...
    Concurrency RouteConcurrency `yaml:"concurrency"`
    CircuitBreaker RouteCircuitBreaker `yaml:"circuit_breaker"`
}
```

---

## 3) Wire into cmd/gateway/main.go

### A) Create per-route runtime objects when you build `routes`

Add maps:

```go
sems := map[string]*mw.Semaphore{}
breakers := map[string]*mw.CircuitBreaker{}
```

When looping over route configs:

```go
sems[rc.Name] = mw.NewSemaphore(rc.Concurrency.MaxInFlight)

breakers[rc.Name] = mw.NewCircuitBreaker(mw.BreakerConfig{
    Enabled: rc.CircuitBreaker.Enabled,
    FailureThreshold: rc.CircuitBreaker.FailureThreshold,
    OpenDuration: time.Duration(rc.CircuitBreaker.OpenSeconds) * time.Second,
    HalfOpenMaxInFlight: rc.CircuitBreaker.HalfOpenMaxInFlight,
})
```

### B) Apply middleware for the matched route

Right after you create `h := base`:

```go
// Protect upstreams (only runs after auth + rate limit because those are outer middleware)
if sem := sems[route.Name]; sem != nil && sem.Enabled() {
    h = mw.ConcurrencyLimit(sem, h)
}
if br := breakers[route.Name]; br != nil {
    h = mw.CircuitBreak(br, h)
}
```

Make sure `RequireAuth` and `RateLimit` wrap OUTSIDE of this, so 401/429 do not affect breaker counts.

---

## 4) Add admin debug endpoints

Set an env var:

- PowerShell:
  `$env:APIGW_ADMIN_KEY = "dev-admin-key"`

Then in `main.go`:

```go
adminKey := os.Getenv("APIGW_ADMIN_KEY")

wrapAdmin := func(h http.Handler) http.Handler {
    h = mw.AccessLog(log, h)
    h = mw.Instrument(metrics, h)
    h = mw.WithRoute(h, "admin")
    h = mw.RequestID(h)
    return mw.RequireAdminKey(adminKey, h)
}

start := time.Now()

mux.Handle("/-/status", wrapAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    info, _ := debug.ReadBuildInfo()
    _ = json.NewEncoder(w).Encode(map[string]any{
        "uptime_seconds": int(time.Since(start).Seconds()),
        "time": time.Now().UTC().Format(time.RFC3339),
        "go_version": func() string { if info != nil { return info.GoVersion }; return "" }(),
    })
})))

mux.Handle("/-/routes", wrapAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    _ = json.NewEncoder(w).Encode(routes)
})))

mux.Handle("/-/auth", wrapAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // if using JWKS validator instance:
    _ = json.NewEncoder(w).Encode(map[string]any{
        "mode": cfg.Auth.Mode,
        "jwks": validator.Stats(),
    })
})))

mux.Handle("/-/limits", wrapAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    out := []map[string]any{}
    for _, rt := range routes {
        row := map[string]any{"route": rt.Name}
        if sem := sems[rt.Name]; sem != nil && sem.Enabled() {
            row["concurrency"] = map[string]any{"max": sem.Cap(), "in_flight": sem.InUse()}
        }
        if br := breakers[rt.Name]; br != nil {
            row["circuit_breaker"] = br.Stats()
        }
        out = append(out, row)
    }
    _ = json.NewEncoder(w).Encode(out)
})))
```

Then call:

```powershell
$headers = @{ "X-Admin-Key" = "dev-admin-key" }
irm http://127.0.0.1:8080/-/status -Headers $headers
```
