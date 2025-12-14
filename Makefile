.PHONY: fmt test run redis

fmt:
	gofmt -w .

test:
	go test ./...

redis:
	docker compose up -d

run:
	go run ./cmd/gateway -config ./config/config.example.yaml
