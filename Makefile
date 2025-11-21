.PHONY: build run test format fmt lint pprof-cpu pprof-mem trace install install-system uninstall completion completion-install completion-install-system

# ===== Variables =====
GO ?= go

GOBIN := $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_MESSAGE ?= $(shell git log -1 --pretty=%s 2>/dev/null || echo unknown)
GIT_MESSAGE_ESC := $(shell printf '%s' "$(GIT_MESSAGE)" | sed "s/'/'\"'\"'/g")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X surge/internal/version.GitCommit=$(GIT_COMMIT) \
	-X 'surge/internal/version.GitMessage=$(GIT_MESSAGE_ESC)' \
	-X surge/internal/version.BuildDate=$(BUILD_DATE)

GOLANGCI_LINT := $(GOBIN)/golangci-lint
GOLANGCI_LINT_VERSION := v1.62.2

# ===== Build =====
build:
	@echo ">> Building surge"
	@$(GO) build -ldflags "$(LDFLAGS)" -o surge ./cmd/surge/

# ===== Run =====
run:
	@echo ">> Running surge $(filter-out $@,$(MAKECMDGOALS))"
	$(GO) run ./cmd/surge/ $(filter-out $@,$(MAKECMDGOALS))

# ===== Test =====
test:
	@echo ">> Running tests"
	$(GO) test ./... --timeout 30s

# ===== Format =====
format: fmt

fmt:
	@echo ">> Formatting code"
	$(GO) fmt ./...

check:
	@echo ">> Checking code"
	$(MAKE) test
	$(MAKE) lint
	./check_file_sizes.sh | grep BAD

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

# ===== Install =====
# Установка в $GOBIN (обычно ~/go/bin или $GOPATH/bin)
# Не требует sudo, но нужно добавить $GOBIN в PATH если его там нет
install: build
	@echo ">> Installing surge to $(GOBIN)"
	@mkdir -p $(GOBIN)
	@cp surge $(GOBIN)/surge
	@echo ">> Installed to $(GOBIN)/surge"
	@echo ">> Make sure $(GOBIN) is in your PATH"

# Системная установка в /usr/local/bin (требует sudo)
install-system: build
	@echo ">> Installing surge to /usr/local/bin (requires sudo)"
	@sudo cp surge /usr/local/bin/surge
	@echo ">> Installed to /usr/local/bin/surge"

# Удаление установленного бинарника
uninstall:
	@echo ">> Removing surge from $(GOBIN)"
	@rm -f $(GOBIN)/surge
	@echo ">> Removed $(GOBIN)/surge"
	@echo ">> To remove system installation, run: sudo rm /usr/local/bin/surge"

# ===== Bash Completion =====
# Генерация bash completion скрипта
# Использует установленный surge если доступен, иначе собирает локально
completion:
	@echo ">> Generating bash completion script"
	@if command -v surge >/dev/null 2>&1; then \
		echo ">> Using installed surge"; \
		surge completion bash > s.sh; \
	else \
		echo ">> Building surge locally"; \
		$(MAKE) build >/dev/null 2>&1; \
		./surge completion bash > s.sh; \
	fi
	@echo ">> Generated s.sh"

# Установка bash completion для текущего пользователя (не требует sudo)
# Устанавливает в ~/.bash_completion.d/ и добавляет source в ~/.bashrc если нужно
completion-install: completion
	@echo ">> Installing bash completion for current user"
	@mkdir -p ~/.bash_completion.d
	@cp s.sh ~/.bash_completion.d/surge
	@if ! grep -q "bash_completion.d/surge" ~/.bashrc 2>/dev/null; then \
		echo "" >> ~/.bashrc; \
		echo "# Surge bash completion" >> ~/.bashrc; \
		echo "source ~/.bash_completion.d/surge" >> ~/.bashrc; \
		echo ">> Added source to ~/.bashrc"; \
	else \
		echo ">> Already configured in ~/.bashrc"; \
	fi
	@echo ">> Bash completion installed to ~/.bash_completion.d/surge"
	@echo ">> Reload shell: source ~/.bashrc or restart terminal"

# Системная установка bash completion (требует sudo)
# Устанавливает в /etc/bash_completion.d/
completion-install-system: completion
	@echo ">> Installing bash completion system-wide (requires sudo)"
	@sudo mkdir -p /etc/bash_completion.d
	@sudo cp s.sh /etc/bash_completion.d/surge
	@echo ">> Installed to /etc/bash_completion.d/surge"
	@echo ">> Completion will be available after restarting terminal"

# Prevent make from trying to build the command arguments as targets
%:
	@:
