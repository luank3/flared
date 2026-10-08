# flared — development tasks
#
# make check   fmt + vet + unit tests (what CI runs)
# make test-integration   live tests against trycloudflare.com (needs network)

GO ?= go
GOFLAGS ?=
COVERAGE_FILE ?= coverage.out

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile the library and the examples
	$(GO) build $(GOFLAGS) ./...

.PHONY: fmt
fmt: ## Format all Go files in place
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt'd
	@unformatted="$$(gofmt -l . | grep -v '^$$' || true)"; \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet, including the integration-tagged files
	$(GO) vet ./...
	$(GO) vet -tags=integration ./...

.PHONY: test
test: ## Run unit tests
	$(GO) test $(GOFLAGS) ./...

.PHONY: test-race
test-race: ## Run unit tests with the race detector and coverage
	$(GO) test $(GOFLAGS) -race -coverprofile="$(COVERAGE_FILE)" ./...

.PHONY: test-integration
test-integration: ## Run integration tests (live network, creates real tunnels)
	$(GO) test $(GOFLAGS) -tags=integration -v -timeout 20m ./...

.PHONY: cover
cover: test-race ## Show the coverage report
	$(GO) tool cover -func="$(COVERAGE_FILE)"

.PHONY: tidy
tidy: ## Tidy and verify module metadata
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy
	$(GO) mod tidy -diff
	$(GO) mod verify

.PHONY: check
check: fmt-check tidy-check vet test-race ## Pre-PR gate: format, module tidiness, vet, unit tests

.PHONY: clean
clean: ## Remove build and coverage artifacts
	$(GO) clean ./...
	rm -f "$(COVERAGE_FILE)"
