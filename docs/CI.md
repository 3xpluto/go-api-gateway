# CI

A typical pipeline for this repo:

- `gofmt` check (fails if formatting changes)
- `go vet`
- `go test ./... -count=1`
- `go test ./... -race -count=1` (Linux only)
- `staticcheck ./...`
- `golangci-lint run ./... --timeout=5m`
- coverage artifact upload

See `.github/workflows/ci.yml`.
