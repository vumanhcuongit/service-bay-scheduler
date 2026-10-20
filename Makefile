DATABASE_URL ?= postgres://scheduler:scheduler@localhost:5432/scheduler?sslmode=disable
GOOSE = go run github.com/pressly/goose/v3/cmd/goose

.PHONY: db-up migrate test run cleanup-keys

db-up:
	docker compose up -d --wait

migrate:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

# -p 1: the concurrency tests commit, so test packages must not interleave.
test:
	go test -p 1 ./...

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

cleanup-keys:
	docker compose exec -T postgres psql -U scheduler -d scheduler -c "DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'"
