# Demo guide

This walkthrough shows off the gateway’s “portfolio” features:
- JWKS JWT auth
- Rate limiting
- Concurrency limits
- Circuit breaker
- Admin endpoints + metrics

## 0) Start upstreams

Two upstreams:
```bash
go run ./cmd/upstream -addr :9001 -name users
go run ./cmd/upstream -addr :9002 -name public
```

## 1) Start gateway

```bash
# PowerShell
$env:APIGW_ADMIN_KEY="dev-admin-key"
go run ./cmd/gateway -config ./config/config.example.yaml
```

## 2) Validate admin endpoints

```bash
curl.exe -i http://127.0.0.1:8080/-/status -H "X-Admin-Key: dev-admin-key"
curl.exe -i http://127.0.0.1:8080/-/routes -H "X-Admin-Key: dev-admin-key"
curl.exe -s http://127.0.0.1:8080/-/limits -H "X-Admin-Key: dev-admin-key"
```

## 3) Rate limit demo (429)

Configure `/public/*` route rate_limit low (example: rps=1 burst=2).
Then:

```bash
hey -n 50 -c 10 http://127.0.0.1:8080/public/hello
```

Expect some `429`.

## 4) Concurrency demo (503 too_busy)

Set public route:
```yaml
concurrency:
  max_in_flight: 1
rate_limit:
  enabled: false
```

Run a burst:
```bash
hey -n 40 -c 20 http://127.0.0.1:8080/public/hello
```

Expect mostly `503`. Confirm body:
```bash
curl.exe -i http://127.0.0.1:8080/public/hello
```
Look for:
```json
{"error":"too_busy", ...}
```

## 5) Circuit breaker demo (503 circuit_open)

Breaker only opens on upstream failures.

Set public route upstream to a dead port:
```yaml
upstream: "http://127.0.0.1:9999"
circuit_breaker:
  enabled: true
  failure_threshold: 2
  open_seconds: 10
  half_open_max_in_flight: 1
concurrency:
  max_in_flight: 50
rate_limit:
  enabled: false
```

Hit sequentially:
```bash
hey -n 10 -c 1 http://127.0.0.1:8080/public/hello
```

You’ll see a mix:
- initial `502` (proxy tries and fails)
- then `503` once breaker opens

Confirm:
```bash
curl.exe -i http://127.0.0.1:8080/public/hello
curl.exe -s http://127.0.0.1:8080/-/limits -H "X-Admin-Key: dev-admin-key"
```

## 6) Metrics

```bash
curl.exe -i http://127.0.0.1:8080/metrics
```
