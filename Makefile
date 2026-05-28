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

# ── Release build with SHA256 checksums ───────────────────────────────────────
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

release: ## Cross-compile release binaries and generate checksums.txt
	@mkdir -p dist
	@echo "Building $(VERSION) for all platforms..."
	GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/brain-linux-amd64       ./cmd/brain
	GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o dist/brain-linux-arm64       ./cmd/brain
	GOOS=darwin  GOARCH=amd64 go build -ldflags="-s -w" -o dist/brain-darwin-amd64      ./cmd/brain
	GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o dist/brain-darwin-arm64      ./cmd/brain
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/brain-windows-amd64.exe ./cmd/brain
	@echo "Generating checksums.txt..."
	@cd dist && sha256sum brain-* > checksums.txt
	@echo "Done. Upload dist/* to the GitHub release."
	@cat dist/checksums.txt
