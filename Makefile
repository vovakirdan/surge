.PHONY: build run test format fmt lint

# ===== Variables =====
GO ?= go

GOBIN := $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

GOLANGCI_LINT := $(GOBIN)/golangci-lint
GOLANGCI_LINT_VERSION := v1.62.2

# ===== Build =====
build:
	@echo ">> Building surge"
	$(GO) build -o surge ./cmd/surge/

# ===== Run =====
run:
	@echo ">> Running surge $(filter-out $@,$(MAKECMDGOALS))"
	$(GO) run ./cmd/surge/ $(filter-out $@,$(MAKECMDGOALS))

# ===== Test =====
test:
	@echo ">> Running tests"
	$(GO) test ./...

# ===== Format =====
format: fmt

fmt:
	@echo ">> Formatting code"
	$(GO) fmt ./...

# ===== Lint =====
$(GOLANGCI_LINT):
	@echo ">> Installing golangci-lint $(GOLANGCI_LINT_VERSION)"
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	@echo ">> Running linters"
	$(GOLANGCI_LINT) run --config .golangci.yaml

# Prevent make from trying to build the command arguments as targets
%:
	@:

