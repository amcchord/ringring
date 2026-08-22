.PHONY: setup dev test check compose-up compose-down

setup:
	go mod download

dev:
	go run ./cmd/ringring

test:
	go test ./...

check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"
	go vet ./...
	go test -race ./...

compose-up:
	docker compose up --build

compose-down:
	docker compose down
