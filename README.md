# go-api-gateway
[![CI](https://github.com/3xpluto/go-api-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/3xpluto/go-api-gateway/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A production-minded **HTTP API gateway** in Go with **JWKS (JWT) auth**, **per-route rate limiting**, **per-route concurrency limits**, a **circuit breaker**, structured logging, Prometheus metrics, and **admin debug endpoints**.

This repo is designed to look great in a portfolio: clean structure, real integration tests, CI-friendly commands, and practical “ops” features you’d expect in a gateway.

---

## Features

- **Reverse proxy routing** by path prefix (`/api/users/*`, `/public/*`, etc.)
- **Auth**
  - Bearer JWT verification via **JWKS URL**
  - Issuer + audience validation (+ leeway)
- **Rate limiting** (per-route)
  - In-memory limiter (works out of the box)
  - Headers exposed (limit/burst/scope/route)
- **Per-route concurrency limits**
  - Fast-fails excess requests with `503` (`too_busy`)
- **Circuit breaker** (per-route)
  - Opens after consecutive upstream failures
  - Half-open probing + auto-close on success
  - Fast-fails with `503` (`circuit_open`) while open
- **Admin debug endpoints** (key-protected)
  - `/-/status`, `/-/routes`, `/-/limits`, `/-/auth`
- **Observability**
  - JSON logs with request IDs + route tags
  - `/metrics` (Prometheus)
  - `/healthz`
- **Testing**
  - Integration tests covering JWKS auth, rate limit, concurrency, circuit breaker
- **CI-ready**
  - gofmt check, vet, tests, race on Linux, staticcheck, golangci-lint

---

## Repo layout

```
cmd/
  gateway/      # main gateway binary
  upstream/     # simple upstream service (dev/demo)
  token/        # mint test JWTs (dev/demo)
  jwksmock/     # JWKS server for local testing (dev/demo)
config/
  config.example.yaml
integration/
  gateway_test.go
internal/
  config/       # config types + loader
  mw/           # middleware (auth, rate limit, breaker, concurrency, metrics)
  proxy/        # route matching + reverse proxy helper
  ratelimit/    # limiter backends (memory, redis if enabled in your build)
  netx/ httpx/  # small net/http helpers
docs/
  DEMO.md
  ARCHITECTURE.md
  DEVELOPMENT.md
  CI.md
  SECURITY.md
scripts/
  loadtest.ps1  # optional helper (Windows)
```

---

## Quickstart

### Prereqs
- Go **1.22+** (tested in CI on Linux)
- Optional: `hey` for load testing

Install `hey`:
```bash
go install github.com/rakyll/hey@latest
```

### 1) Run example upstreams (dev)
In separate terminals:

```bash
go run ./cmd/upstream -addr :9001 -name users
go run ./cmd/upstream -addr :9002 -name public
```

### 2) Run the gateway
```bash
# PowerShell:
$env:APIGW_ADMIN_KEY="dev-admin-key"
go run ./cmd/gateway -config ./config/config.example.yaml

# bash/zsh:
export APIGW_ADMIN_KEY="dev-admin-key"
go run ./cmd/gateway -config ./config/config.example.yaml
```

### 3) Hit endpoints
Public route:
```bash
curl -i http://127.0.0.1:8080/public/hello
```

Health + metrics:
```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/metrics
```

Admin endpoints (require key):
```bash
curl -i http://127.0.0.1:8080/-/status -H "X-Admin-Key: dev-admin-key"
curl -i http://127.0.0.1:8080/-/routes -H "X-Admin-Key: dev-admin-key"
curl -i http://127.0.0.1:8080/-/limits -H "X-Admin-Key: dev-admin-key"
```

> **Windows tip:** In Windows PowerShell, `curl` is an alias for `Invoke-WebRequest`. Use `curl.exe` to call real curl:
>
> ```powershell
> curl.exe -i http://127.0.0.1:8080/public/hello
> ```

---

## Auth (JWKS)

When a route has `auth_required: true`, the gateway expects:

```
Authorization: Bearer <JWT>
```

The validator fetches keys from your configured JWKS URL and validates:
- signature (RS256, etc.)
- issuer (`iss`)
- audience (`aud`)
- expiry (`exp`) with a leeway window

Local testing: see `cmd/jwksmock` + `cmd/token` or `docs/DEMO.md`.

---

## Load testing (rate limit + concurrency)

Example (will likely trigger 429/503 depending on config):

```bash
hey -n 200 -c 50 http://127.0.0.1:8080/public/hello
```

Inspect protections:
- `429` → rate limit
- `503` with body `{"error":"too_busy"}` → concurrency limit
- `503` with body `{"error":"circuit_open"}` → circuit breaker

Live state:
```bash
curl -s http://127.0.0.1:8080/-/limits -H "X-Admin-Key: dev-admin-key"
```

---

## Development commands

Format:
```bash
gofmt -w .
git diff --exit-code
```

Tests:
```bash
go test ./... -count=1
```

Coverage (recommended: counts integration tests too):
```bash
go test ./... -count=1 -coverpkg=./... -coverprofile=coverage.out
go tool cover -func coverage.out
go tool cover -html coverage.out -o coverage.html
```

Staticcheck:
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

golangci-lint:
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run ./... --timeout=5m
```

Race detector:
- Run in CI on **Linux** (recommended)
- On Windows, `-race` requires CGO + a compatible C toolchain (see `docs/DEVELOPMENT.md`)

---

## Configuration

See `config/config.example.yaml`. Core ideas:
- **routes**: match `path_prefix`, proxy to `upstream`, optional `strip_prefix`
- per-route **rate_limit**, **concurrency**, and **circuit_breaker**
- **auth**: configure JWKS URL + issuers/audiences/algs

See `docs/ARCHITECTURE.md` for details.

---

## License

MIT.
