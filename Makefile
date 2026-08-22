.PHONY: setup dev test check backup restore-drill sip-smoke compose-up compose-down

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

backup:
	./scripts/backup.sh

restore-drill:
	test -n "$(BACKUP)"
	./scripts/restore-drill.sh "$(BACKUP)"

sip-smoke:
	./scripts/sip-smoke.sh

compose-up:
	docker compose up --build

compose-down:
	docker compose down
