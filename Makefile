.PHONY: fmt test vet run

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

run:
	go run ./cmd/gateway -config ./config/config.example.yaml
