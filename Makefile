DATABASE_URL ?= postgres://scheduler:scheduler@localhost:5432/scheduler?sslmode=disable
# @version builds goose outside this module, so its many drivers stay out of go.mod. Keep in step with go.mod.
GOOSE = go run github.com/pressly/goose/v3/cmd/goose@v3.22.1
# Keep in step with the version in .github/workflows/ci.yml.
GOLANGCI_LINT_VERSION = v2.15.0
# A prebuilt binary, so linting needs no newer Go toolchain than go.mod's. The path carries the version, so a bump reinstalls.
GOLANGCI_LINT = bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: db-up migrate vet layers lint test races ci run docker-build cleanup-keys

db-up:
	docker compose up -d --wait

migrate:
	$(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

vet:
	go vet ./...

# Fails on an import that breaks handler → service → model, repository → model.
layers:
	@out=$$( { go list -deps ./internal/model ./internal/service | grep -E '^(net/http|database/sql)$$|jackc|internal/(handler|repository)'; \
		go list -deps ./internal/repository | grep -E 'internal/(service|handler)'; \
		go list -deps ./internal/handler | grep -E 'jackc|internal/repository'; \
		go list -deps ./internal/clock | grep -E 'internal/(model|service|handler|repository)'; } ); \
	if [ -n "$$out" ]; then echo "layer violation:"; echo "$$out"; exit 1; fi

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

# The official install script at the same tag; it verifies the release checksum.
$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b bin $(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

# -p 1: the concurrency tests commit, so test packages must not interleave.
test:
	go test -p 1 ./...

# The pool tests race real commits; 20 runs in a row, so a pass is not luck.
races:
	go test ./spectest/ -run 'TestM17|TestM25|TestN3[67]|TestTenRacers|TestVehicleAtTwoDealerships' -count=20

ci: vet layers lint test races

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

docker-build:
	docker build -t service-bay-scheduler .

cleanup-keys:
	docker compose exec -T postgres psql -U scheduler -d scheduler -c "DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'"
