# apigw (Go)

A small API gateway / reverse proxy with:
- Path-prefix routing (longest prefix match)
- JWT auth (HS256 for v1)
- Per-route rate limiting (Redis token bucket, with in-memory fallback)
- Structured JSON logs
- Prometheus metrics at `/metrics`
- Health check at `/healthz`

## Run locally

### 1) Start Redis
```bash
docker compose up -d
```

### 2) (Optional) start example upstream servers
```bash
go run ./cmd/upstream -addr :9001 -name users
go run ./cmd/upstream -addr :9002 -name public
```

### 3) Run the gateway
```bash
go run ./cmd/gateway -config ./config/config.example.yaml
```

Gateway: http://127.0.0.1:8080  
Metrics: http://127.0.0.1:8080/metrics

## Try it

Public route:
```bash
curl -i http://127.0.0.1:8080/public/hello
```

Generate a dev token:
```bash
go run ./cmd/token -secret dev-secret -sub user_123
```

Protected route:
```bash
TOKEN="paste-token"
curl -i http://127.0.0.1:8080/api/users/me -H "Authorization: Bearer $TOKEN"
```

## CI
A GitHub Actions workflow is included at `.github/workflows/ci.yml` (gofmt + go test).
