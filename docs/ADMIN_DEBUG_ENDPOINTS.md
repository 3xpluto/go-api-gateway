# Admin debug endpoints

These endpoints are **disabled unless** you set an admin key.

## Enable
Set an environment variable before running the gateway:

- Windows PowerShell:
  `$env:APIGW_ADMIN_KEY = "dev-admin-key"`

Then send the key as a header:

- `X-Admin-Key: dev-admin-key`

## Endpoints

- `GET /-/status`
  - uptime + version/build info + current time

- `GET /-/routes`
  - route table (match prefix, upstream, auth, rate limit)

- `GET /-/auth`
  - auth mode and (if JWKS) last refresh + key count

- `GET /-/limits`
  - per-route concurrency (in-flight) + circuit breaker state
