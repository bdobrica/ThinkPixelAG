SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO ?= go
NPM_EXEC ?= npx --yes
OPENAPI_CLI_VERSION := 2.3.0
OPENAPI_CLI := $(NPM_EXEC) @redocly/cli@$(OPENAPI_CLI_VERSION)
OPA ?= opa
DOCKER ?= docker
COMPOSE ?= $(DOCKER) compose

MODULE := github.com/bdobrica/ThinkPixelAG
BUILD_DIR ?= .cache/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
IMAGE ?= thinkpixelag:dev
GO_FILES := $(shell git ls-files '*.go')
.PHONY: help tools generate generate-check fmt fmt-check lint test test-race \
	test-policy test-integration test-e2e dependency-check vulnerability-check \
	license-check security build image verify clean compose-check dev-up \
	dev-up-valkey dev-status dev-smoke dev-down dev-reset

help: ## Show the stable development targets.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tools: ## Download and identify all exactly pinned verification tools.
	$(GO) mod download
	$(GO) mod download -modfile=tools/go.mod
	$(GO) tool -modfile=tools/go.mod govulncheck -version
	$(GO) tool -modfile=tools/go.mod go-licenses --help >/dev/null 2>&1
	$(OPENAPI_CLI) --version

generate: ## Run all Go generators.
	$(GO) generate ./...

generate-check: generate ## Fail when generation changes tracked files.
	@git diff --exit-code -- .
	@untracked="$$(git ls-files --others --exclude-standard)"; \
	if [[ -n "$$untracked" ]]; then printf 'Generation left untracked files:\n%s\n' "$$untracked" >&2; exit 1; fi

fmt: ## Format tracked Go source files.
	gofmt -w $(GO_FILES)

fmt-check: ## Fail when tracked Go source files are not formatted.
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [[ -n "$$unformatted" ]]; then printf 'Go files require formatting:\n%s\n' "$$unformatted" >&2; exit 1; fi

lint: fmt-check ## Run static analysis, read-only module checks, and OpenAPI validation.
	$(GO) vet ./...
	cd tools && $(GO) vet ./...
	$(GO) mod verify
	$(GO) list -mod=readonly ./... >/dev/null
	cd tools && $(GO) mod verify
	cd tools && $(GO) list -mod=readonly ./... >/dev/null
	$(OPENAPI_CLI) lint api/openapi/thinkpixelag.yaml

test: ## Run unit and repository contract tests with coverage.
	$(GO) test -cover ./...
	cd tools && $(GO) test -cover ./...

test-race: ## Run all Go tests with the race detector.
	$(GO) test -race ./...
	cd tools && $(GO) test -race ./...

test-policy: ## Run OPA/Rego tests when policy sources are present.
	@rego_files="$$(git ls-files 'policies/*.rego' 'policies/**/*.rego')"; \
	if [[ -z "$$rego_files" ]]; then printf 'test-policy: no Rego sources yet (policy implementation is scheduled for Phase 3)\n'; \
	else command -v "$(OPA)" >/dev/null || { printf 'test-policy: %s is required\n' "$(OPA)" >&2; exit 1; }; "$(OPA)" test policies; fi

test-integration: ## Run integration-tagged Go tests (suite grows in later phases).
	$(GO) test -tags=integration ./...

test-e2e: ## Run end-to-end-tagged Go tests (suite grows in later phases).
	$(GO) test -tags=e2e ./...

compose-check: ## Validate the pinned local dependency stack definition.
	$(COMPOSE) -f compose.yaml config --quiet

dev-up: compose-check ## Start healthy PostgreSQL and OPA local dependencies.
	$(COMPOSE) -f compose.yaml up --detach --wait postgres opa

dev-up-valkey: compose-check ## Start healthy PostgreSQL, OPA, and optional Valkey.
	$(COMPOSE) -f compose.yaml --profile valkey up --detach --wait postgres opa valkey

dev-status: ## Show local dependency container and health state.
	$(COMPOSE) -f compose.yaml --profile valkey ps

dev-smoke: ## Verify running dependency versions, health, auth, and isolation basics.
	$(COMPOSE) -f compose.yaml run --rm --no-deps postgres sh -eu -c 'PGPASSWORD="$$POSTGRES_PASSWORD" psql -h postgres -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -Atqc "select current_setting('"'"'server_version'"'"'), current_user, current_database()"'
	@$(COMPOSE) -f compose.yaml run --rm --no-deps postgres sh -eu -c '! PGPASSWORD=definitely-wrong psql -h postgres -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -Atqc "select 1" >/dev/null 2>&1'
	@opa_address="$$( $(COMPOSE) -f compose.yaml port opa 8181 )"; curl --fail --silent --show-error "http://$$opa_address/health" >/dev/null
	$(COMPOSE) -f compose.yaml exec -T opa /opa version
	@if $(COMPOSE) -f compose.yaml --profile valkey ps --status running --services | grep -qx valkey; then \
		$(COMPOSE) -f compose.yaml exec -T valkey sh -eu -c 'test "$$(VALKEYCLI_AUTH="$$VALKEY_PASSWORD" valkey-cli ping)" = PONG; test "$$(VALKEYCLI_AUTH=definitely-wrong valkey-cli ping 2>/dev/null)" != PONG; printf "PONG\n"'; \
	else printf 'dev-smoke: optional Valkey profile is not running\n'; fi

dev-down: ## Stop local dependencies while preserving PostgreSQL state.
	$(COMPOSE) -f compose.yaml --profile valkey down --remove-orphans

dev-reset: ## Stop dependencies and delete only this Compose project's local volumes.
	$(COMPOSE) -f compose.yaml --profile valkey down --volumes --remove-orphans

dependency-check: ## Enforce reviewed module sources and exact-version policy.
	cd tools && $(GO) run ./cmd/dependencycheck -module-dir .. -policy ../dependency-policy.json

vulnerability-check: ## Scan reachable application and test code with govulncheck.
	$(GO) tool -modfile=tools/go.mod govulncheck -test ./...

license-check: ## Enforce application and test dependency license policy.
	$(GO) tool -modfile=tools/go.mod go-licenses check --include_tests \
		--ignore $(MODULE) \
		--disallowed_types=forbidden,restricted,reciprocal,unknown ./...

security: dependency-check vulnerability-check license-check ## Run dependency security and license gates.

build: ## Build the static governance-plane binary with version metadata.
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false \
		-ldflags '-s -w -X main.version=$(VERSION) -X main.revision=$(REVISION)' \
		-o $(BUILD_DIR)/thinkpixelag ./cmd/thinkpixelag

image: ## Build the OCI image once ENG-011 provides the Dockerfile.
	@test -f Dockerfile || { printf 'image: Dockerfile is not implemented yet; complete ENG-011 first\n' >&2; exit 2; }
	$(DOCKER) build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t $(IMAGE) .

verify: generate-check lint test test-race test-policy test-integration test-e2e compose-check security build ## Run the complete non-runtime clean-checkout gate.

clean: ## Remove repository-local build outputs.
	rm -rf .cache/bin
