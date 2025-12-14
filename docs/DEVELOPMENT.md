# Development

## Windows PowerShell notes

- In Windows PowerShell, `curl` is an alias for `Invoke-WebRequest`.
  Use `curl.exe` to call real curl.

Examples:
```powershell
curl.exe -i http://127.0.0.1:8080/public/hello
curl.exe -i http://127.0.0.1:8080/-/status -H "X-Admin-Key: dev-admin-key"
```

## Linting

Install:
```powershell
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
$env:PATH += ";$env:USERPROFILE\go\bin"
```

Run:
```powershell
staticcheck ./...
golangci-lint run ./... --timeout=5m
```

## Tests

```powershell
go test ./... -count=1
```

Coverage (recommended):
```powershell
go test ./... -count=1 -coverpkg=./... -coverprofile=coverage.out
go tool cover -func coverage.out
go tool cover -html coverage.out -o coverage.html
```

## Race detector

Recommended: run `-race` in CI on Ubuntu.

On Windows, `-race` requires CGO and a compatible C toolchain. Toolchain combos can be brittle on Windows, so CI is the default.
