.PHONY: setup dev test check admin-test backup restore-drill sip-smoke sip-tls-smoke nat-smoke linphone-smoke radio-smoke compose-up compose-down

setup:
	go mod download

dev:
	go run ./cmd/ringring

test:
	go test ./...

check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"
	sh -n ringringctl $$(find scripts -name '*.sh' -type f)
	./scripts/ringringctl-test.sh
	./scripts/sip-tls-sync-test.sh
	go vet ./...
	go test -race ./...

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
