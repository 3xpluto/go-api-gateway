# Security notes

This repo is a demo gateway and not a full security product, but it follows good practices:

- JWT auth via JWKS with:
  - allowed algorithms
  - issuer/audience allowlists
  - cache + timeouts
- Admin endpoints are protected by `X-Admin-Key`
  - intended for internal debugging only
  - recommended to bind admin endpoints to internal networks in real deployments
- Rate limiting + concurrency limits reduce abuse and resource exhaustion
- Circuit breaker prevents repeated upstream failures from cascading

For production use, consider:
- mTLS between gateway and upstreams
- structured audit logs
- IP allowlists for admin endpoints
- secrets management (avoid env vars in long-lived environments)
