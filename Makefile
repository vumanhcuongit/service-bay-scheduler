DATABASE_URL ?= postgres://scheduler:scheduler@localhost:5432/scheduler?sslmode=disable
GOOSE = go run github.com/pressly/goose/v3/cmd/goose
# Keep in step with the version in .github/workflows/ci.yml.
GOLANGCI_LINT_VERSION = v2.15.0
GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: db-up migrate vet lint test ci run cleanup-keys

db-up:
	docker compose up -d --wait

migrate:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

vet:
	go vet ./...

# Runs the pinned linter through go run, so no local install is needed.
lint:
	$(GOLANGCI_LINT) run ./...

# -p 1: the concurrency tests commit, so test packages must not interleave.
test:
	go test -p 1 ./...

ci: vet lint test

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

cleanup-keys:
	docker compose exec -T postgres psql -U scheduler -d scheduler -c "DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'"
