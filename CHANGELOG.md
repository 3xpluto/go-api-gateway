# Changelog

All notable changes to this project will be documented in this file.

This project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]
### Added
- _TBD_

### Changed
- _TBD_

### Fixed
- _TBD_

---

## [v1.0.0] - 2025-12-14
### Added
- Reverse proxy routing by path prefix with optional strip-prefix behavior.
- JWT auth via JWKS URL (issuer/audience validation, allowed algs, caching, timeouts).
- Per-route rate limiting with informative headers and 429 responses.
- Per-route concurrency limits with fast-fail 503 `too_busy`.
- Per-route circuit breaker with open/half-open behavior and 503 `circuit_open`.
- Admin debug endpoints (X-Admin-Key protected): `/-/status`, `/-/routes`, `/-/limits`, `/-/auth`.
- Prometheus metrics endpoint (`/metrics`) and health endpoint (`/healthz`).
- Structured JSON logging including request IDs and route tagging.
- Integration tests covering JWKS auth and rate limit behavior.
- CI pipeline with gofmt/vet/tests + staticcheck + golangci-lint (race on Linux).

[Unreleased]: https://github.com/3xpluto/go-api-gateway/compare/v1.0.0...HEAD
[v1.0.0]: https://github.com/3xpluto/go-api-gateway/releases/tag/v1.0.0
