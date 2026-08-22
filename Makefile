.PHONY: setup dev test check security security-contract admin-test backup restore-drill sip-smoke sip-tls-smoke nat-smoke linphone-smoke radio-smoke compose-up compose-down

GOVULNCHECK_VERSION ?= v1.7.0

setup:
	go mod download

dev:
	go run ./cmd/ringring

test:
	go test ./...

check: security-contract
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"
	sh -n ringringctl $$(find scripts -name '*.sh' -type f)
	./scripts/ringringctl-test.sh
	./scripts/sip-tls-sync-test.sh
	go vet ./...
	go test -race ./...

security: security-contract
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

security-contract:
	go test ./internal/securitycontract -count=1

admin-test:
	sh -n ringringctl $$(find scripts -name '*.sh' -type f)
	./scripts/ringringctl-test.sh
	./scripts/sip-tls-sync-test.sh

backup:
	./scripts/backup.sh

restore-drill:
	test -n "$(BACKUP)"
	./scripts/restore-drill.sh "$(BACKUP)"

sip-smoke:
	./scripts/sip-smoke.sh

sip-tls-smoke: sip-smoke linphone-smoke

nat-smoke:
	./scripts/nat-smoke.sh

linphone-smoke:
	./scripts/linphone-smoke.sh

radio-smoke:
	./scripts/radio-smoke.sh

compose-up:
	docker compose up --build

compose-down:
	docker compose down
