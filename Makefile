.PHONY: build run test format fmt lint pprof-cpu pprof-mem trace

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

# ===== Profiling helpers =====
pprof-cpu:
	$(GO) run ./cmd/surge diag --cpu-profile=cpu.pprof ./test.sg
	go tool pprof -http=:8081 cpu.pprof

pprof-mem:
	$(GO) run ./cmd/surge diag --mem-profile=mem.pprof ./test.sg
	go tool pprof -http=:8082 mem.pprof

trace:
	$(GO) run ./cmd/surge diag --trace=trace.out ./test.sg
	$(GO) tool trace trace.out

# Prevent make from trying to build the command arguments as targets
%:
	@:
