# Configuration Guide

This gateway is configured via a single YAML file (see `config/config.example.yaml`).

## server

- `addr` (string): Listen address (e.g. `:8080`).
- `trusted_proxies` (list[string]): CIDRs that are allowed to supply `X-Forwarded-For`.
  - If empty, the gateway ignores `X-Forwarded-For` and uses `RemoteAddr`.
  - Example: `["10.0.0.0/8", "192.168.0.0/16"]`
- `max_header_bytes` (int): Maximum request header size.
- `max_body_bytes` (int): Maximum request body size.
- `read_header_timeout_seconds` (int): Time allowed to read request headers.
- `read_timeout_seconds` (int): Time allowed to read the full request.
- `write_timeout_seconds` (int): Time allowed to write the response.
- `idle_timeout_seconds` (int): Idle keep-alive timeout.

## upstream

Controls the reverse-proxy transport:

- `dial_timeout_seconds`
- `tls_handshake_timeout_seconds`
- `response_header_timeout_seconds`
- `idle_conn_timeout_seconds`
- `max_idle_conns`
- `max_idle_conns_per_host`

## auth

v1 supports HS256 JWT:

- `mode`: `"hmac"`
- `hmac_secret`: shared secret

## rate_limit

- `backend`: `"redis"` or `"memory"`
- `redis.addr/password/db`
- `memory.cleanup_seconds/ttl_seconds`

## routes[]

Each route uses **longest path prefix match**.

- `name`: Unique route name (used in metrics + logs + rate limit keys)
- `match.path_prefix`: Path prefix to match (must start with `/`)
- `upstream`: Upstream base URL (e.g. `http://127.0.0.1:9001`)
- `strip_prefix`: Optional prefix removed before forwarding (e.g. `/api`)
- `auth_required`: Require JWT on this route
- `rate_limit`: Per-route limiter settings
  - `enabled`: bool
  - `rps`: float (tokens per second)
  - `burst`: float (bucket capacity)
  - `scope`: `"ip"` or `"user"`
