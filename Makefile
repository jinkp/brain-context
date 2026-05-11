test:
	go test ./...

test-unit: ## Run unit tests only (no integration)
	go test ./... -v

test-integration: ## Run integration tests (requires postgres)
	go test -tags=integration ./... -v

build-cli:
	go build -o ./bin/brain ./cmd/brain

run-api:
	go run ./cmd/api

db-up:
	docker compose up -d postgres

smoke: ## Run local smoke test
	./scripts/smoke_local.sh
