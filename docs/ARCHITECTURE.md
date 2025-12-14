# Architecture

## Request flow

Incoming request:
1) Route match (path prefix)
2) (Optional) Auth (Bearer JWT via JWKS)
3) (Optional) Rate limit (per route / per scope)
4) (Optional) Concurrency limit (per route)
5) (Optional) Circuit breaker (per route)
6) Reverse proxy to upstream
7) Logging, metrics, request ID, route tagging

## Routing

Routes are configured with:
- `name`
- `path_prefix`
- `upstream`
- `strip_prefix` (optional)

A request `/api/users/me` might:
- match route `users` with `path_prefix: /api/users/`
- strip `/api` before proxying, so upstream receives `/users/me`

## Auth (JWKS)

The gateway validates bearer JWTs using:
- JWKS URL (cached)
- allowed algorithms
- issuer allowlist
- audience allowlist
- expiration (+ leeway)

Auth is enabled per route using `auth_required: true`.

## Rate limiting

Rate limit runs per route and can scope by:
- `ip`
- `user` (subject / validated identity)
- (extendable)

It returns `429` with informative headers:
- `X-RateLimit-Limit-Rps`
- `X-RateLimit-Burst`
- `X-RateLimit-Scope`
- `X-RateLimit-Route`

## Concurrency limits

Concurrency limits are per-route semaphores:
- if `max_in_flight` is exceeded, request fails fast with `503` and body `{ "error": "too_busy" }`

## Circuit breaker

Per-route breaker:
- tracks consecutive upstream failures (5xx / transport failures)
- opens after `failure_threshold`
- while open: fails fast with `503` and body `{ "error": "circuit_open" }`
- after `open_seconds`: half-open, allows a small number of probes
- closes on successful probe

## Admin endpoints

Key-protected endpoints under `/-/`:
- `/-/status`: basic runtime status
- `/-/routes`: loaded route config summary
- `/-/limits`: per-route breaker + concurrency snapshot
- `/-/auth`: auth/JWKS status

Admin endpoints are hidden/disabled when `APIGW_ADMIN_KEY` is unset (by design).
