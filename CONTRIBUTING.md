# Contributing

Thanks for taking the time to contribute! This project is intentionally kept clean and CI-friendly.

## Requirements

- Go 1.22+ (CI runs on Linux)
- Optional tools:
  - staticcheck
  - golangci-lint
  - hey (load testing)

Install optional tools:
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/rakyll/hey@latest
```

## Project layout

- `cmd/` — binaries (gateway + dev helpers)
- `internal/` — private packages used by binaries
- `integration/` — integration tests (end-to-end style)
- `config/` — example configs
- `docs/` — documentation
- `.github/workflows/` — CI

## Development workflow

### Format
```bash
gofmt -w .
git diff --exit-code
```

### Vet + tests
```bash
go vet ./...
go test ./... -count=1
```

### Coverage
```bash
go test ./... -count=1 -coverpkg=./... -coverprofile=coverage.out
go tool cover -func coverage.out
go tool cover -html coverage.out -o coverage.html
```

### Lint
```bash
staticcheck ./...
golangci-lint run ./... --timeout=5m
```

### Race detector
The race detector is run in CI on Linux.
On Windows, `-race` may require CGO and a compatible C toolchain.

## Commit style

Use short, descriptive commit messages. Conventional Commits are recommended:
- `feat: ...`
- `fix: ...`
- `docs: ...`
- `chore: ...`
- `test: ...`
- `refactor: ...`

## Pull request checklist

- [ ] `gofmt` clean (`git diff --exit-code`)
- [ ] `go test ./... -count=1` passes
- [ ] lint passes (staticcheck + golangci-lint) or explain why not
- [ ] docs updated (if behavior/config changed)
- [ ] tests added/updated (especially for middleware changes)

## Security issues

Please do not open public issues for security vulnerabilities.
Open a private report (or contact the maintainer) with:
- impact + reproduction steps
- affected endpoints/configs
- suggested mitigation
