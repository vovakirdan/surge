.PHONY: build run test vet sec format fmt lint staticcheck pprof-cpu pprof-mem trace install install-system uninstall uninstall-system completion completion-install completion-install-system
.PHONY: golden golden-update golden-check stats
.PHONY: c-check cfmt-check c-warnings ctidy cppcheck

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
GOLANGCI_LINT_VERSION := v2.7.2

STATICCHECK := $(GOBIN)/staticcheck
GOSEC := $(GOBIN)/gosec

# ===== C Runtime Variables =====
CC ?= clang
CXX ?= clang++
C_RUNTIME_DIR := runtime/native
C_SOURCES := $(shell find $(C_RUNTIME_DIR) -name '*.c' 2>/dev/null)
C_HEADERS := $(shell find $(C_RUNTIME_DIR) -name '*.h' 2>/dev/null)
C_FILES := $(C_SOURCES) $(C_HEADERS)

# Strict warning flags for C compilation
C_WARN_FLAGS := -Wall -Wextra -Wpedantic -Werror \
	-Wshadow -Wconversion -Wsign-conversion -Wcast-qual -Wcast-align \
	-Wstrict-prototypes -Wmissing-prototypes -Wold-style-definition \
	-Wformat=2 -Wundef -Wdouble-promotion -fno-common

C_STD := -std=c11
C_INCLUDES := -I$(C_RUNTIME_DIR)

# ===== OS Detection =====
# Определение операционной системы
UNAME_S := $(shell uname -s 2>/dev/null || echo "Unknown")
ifeq ($(UNAME_S),Darwin)
	OS := darwin
	# На macOS используем стандартные пути
	SYSTEM_BINDIR := /usr/local/bin
	SYSTEM_SHAREDIR := /usr/local/share/surge
	# На macOS нет /etc/profile.d/, используем /etc/paths.d/ для PATH
	# Для переменных окружения лучше использовать ~/.zshrc или ~/.bash_profile
	PROFILE_DIR := /etc/paths.d
	PROFILE_FILE := /etc/paths.d/surge
else
	# Linux и другие Unix-подобные системы
	OS := linux
	SYSTEM_BINDIR := /usr/local/bin
	SYSTEM_SHAREDIR := /usr/local/share/surge
	PROFILE_DIR := /etc/profile.d
	PROFILE_FILE := /etc/profile.d/surge.sh
endif

# ===== Build =====
build:
	@echo ">> Building surge"
	@rm -f surge
	@$(GO) build -ldflags "$(LDFLAGS)" -o surge ./cmd/surge/

# ===== Run =====
run:
	@echo ">> Running surge $(filter-out $@,$(MAKECMDGOALS))"
	$(GO) run ./cmd/surge/ $(filter-out $@,$(MAKECMDGOALS))

# ===== Vet =====
vet:
	@echo ">> Running vet"
	$(GO) vet ./...

sec:
	@echo ">> Running gosec"
	$(GOSEC) ./...

# ===== Test =====
test:
	@echo ">> Running tests"
	$(GO) test ./... --timeout 30s

# ===== Format =====
format: fmt

fmt:
	@echo ">> Formatting code"
	$(GO) fmt ./...

golden: golden-check

golden-update: build
	@./scripts/golden_update.sh

golden-check: golden-update
	@if ! git diff --quiet -- testdata/golden; then \
		echo "Golden files are out of date. Run 'make golden-update' and commit changes."; \
		git diff -- testdata/golden; \
		exit 1; \
	fi

check:
	@echo ">> Checking code"
	$(MAKE) test
	$(MAKE) lint
	$(MAKE) c-check
	@echo ">> Checking file sizes"
	@echo "It may take a while... please wait..."
	./check_file_sizes.sh | grep BAD || echo "No files need refactoring"

# ===== Lint =====
$(GOLANGCI_LINT):
	@echo ">> Installing golangci-lint $(GOLANGCI_LINT_VERSION)"
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	@echo ">> Running linters"
	$(GOLANGCI_LINT) run --config .golangci.yaml

# ===== Staticcheck =====
$(STATICCHECK):
	@echo ">> Installing staticcheck"
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest

staticcheck: $(STATICCHECK)
	@echo ">> Running staticcheck"
	$(STATICCHECK) ./...

# ===== C Runtime Checks =====
# Check C code formatting with clang-format
cfmt-check:
	@echo ">> Checking C code formatting"
	@if ! command -v clang-format >/dev/null 2>&1; then \
		echo "Error: clang-format not found. Install with: sudo apt-get install -y clang-format"; \
		exit 1; \
	fi
	@failed=0; \
	for file in $(C_FILES); do \
		if [ -f "$$file" ]; then \
			if ! clang-format --dry-run --Werror "$$file" >/dev/null 2>&1; then \
				echo "Formatting error in $$file"; \
				clang-format "$$file" | diff -u "$$file" - || true; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "C code formatting check failed. Run 'clang-format -i' on the files above."; \
		exit 1; \
	fi
	@echo ">> C code formatting OK"

# Compile C code with strict warnings
c-warnings:
	@echo ">> Compiling C runtime with strict warnings"
	@if ! command -v $(CC) >/dev/null 2>&1; then \
		echo "Error: $(CC) not found. Install with: sudo apt-get install -y clang llvm"; \
		exit 1; \
	fi
	@failed=0; \
	tmpdir=$$(mktemp -d); \
	trap "rm -rf $$tmpdir" EXIT; \
	for src in $(C_SOURCES); do \
		if [ -f "$$src" ]; then \
			obj=$$tmpdir/$$(basename $$src .c).o; \
			if ! $(CC) $(C_STD) $(C_WARN_FLAGS) $(C_INCLUDES) -c "$$src" -o "$$obj" 2>&1; then \
				echo "Compilation failed for $$src"; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "C code compilation with strict warnings failed"; \
		exit 1; \
	fi
	@echo ">> C code compilation with strict warnings OK"

# Run clang-tidy on C code
ctidy:
	@echo ">> Running clang-tidy on C code"
	@if ! command -v clang-tidy >/dev/null 2>&1; then \
		echo "Error: clang-tidy not found. Install with: sudo apt-get install -y clang-tidy"; \
		exit 1; \
	fi
	@failed=0; \
	for file in $(C_SOURCES); do \
		if [ -f "$$file" ]; then \
			output=$$(clang-tidy "$$file" --config-file=.clang-tidy -- $(C_STD) $(C_INCLUDES) 2>&1); \
			if echo "$$output" | grep -qE "(error|warning):"; then \
				echo "clang-tidy found issues in $$file:"; \
				echo "$$output"; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "clang-tidy check failed"; \
		exit 1; \
	fi
	@echo ">> clang-tidy check OK"

# Run cppcheck on C code
cppcheck:
	@echo ">> Running cppcheck on C code"
	@if ! command -v cppcheck >/dev/null 2>&1; then \
		echo "Error: cppcheck not found. Install with: sudo apt-get install -y cppcheck"; \
		exit 1; \
	fi
	@if [ -z "$(C_SOURCES)" ]; then \
		echo "No C sources found"; \
		exit 0; \
	fi
	@cppcheck --enable=warning,style,performance,portability \
		--error-exitcode=1 \
		--suppress=missingIncludeSystem \
		--suppress=unusedFunction \
		--std=c11 \
		$(C_INCLUDES) \
		$(C_SOURCES) || exit 1
	@echo ">> cppcheck OK"

# Run all C code checks
c-check: cfmt-check c-warnings
	@echo ">> All C runtime checks passed"

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

# ===== Statistics =====
stats:
	@./scripts/code_stats.sh

# ===== Install =====
# Установка в $GOBIN (обычно ~/go/bin или $GOPATH/bin)
# Не требует sudo, но нужно добавить $GOBIN в PATH если его там нет
install: build
	@echo ">> Installing surge to $(GOBIN)"
	@mkdir -p $(GOBIN)
	@cp surge $(GOBIN)/surge
	@echo ">> Installed to $(GOBIN)/surge"
	@echo ">> Make sure $(GOBIN) is in your PATH"

# Системная установка (требует sudo)
# Автоматически определяет правильные пути для macOS и Linux
install-system: build
	@echo ">> Detected OS: $(OS)"
	@echo ">> Installing surge to $(SYSTEM_BINDIR) (requires sudo)"
	@sudo mkdir -p $(SYSTEM_BINDIR)
	@sudo cp surge $(SYSTEM_BINDIR)/surge
	@echo ">> Installing standard library to $(SYSTEM_SHAREDIR) (requires sudo)"
	@sudo mkdir -p $(SYSTEM_SHAREDIR)
	@sudo cp -r core stdlib $(SYSTEM_SHAREDIR)/
ifeq ($(OS),darwin)
	@echo ">> On macOS, add to ~/.zshrc or ~/.bash_profile:"
	@echo ">>   export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"
else
	@echo ">> Writing $(PROFILE_FILE) to export SURGE_STDLIB if unset"
	@sudo mkdir -p $(PROFILE_DIR)
	@sudo sh -c 'printf "# surge stdlib path\n: \$${SURGE_STDLIB:=$(SYSTEM_SHAREDIR)}\nexport SURGE_STDLIB\n" > $(PROFILE_FILE)'
endif
	@echo ">> Installed to $(SYSTEM_BINDIR)/surge"
	@echo ">> For current shell run: export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"

# Удаление установленного бинарника из $GOBIN
uninstall:
	@echo ">> Removing surge from $(GOBIN)"
	@rm -f $(GOBIN)/surge
	@echo ">> Removed $(GOBIN)/surge"
	@echo ">> To remove system installation, run: make uninstall-system"

# Удаление системной установки (требует sudo)
# Автоматически определяет правильные пути для macOS и Linux
uninstall-system:
	@echo ">> Detected OS: $(OS)"
	@echo ">> Removing surge from $(SYSTEM_BINDIR) (requires sudo)"
	@sudo rm -f $(SYSTEM_BINDIR)/surge
	@echo ">> Removing standard library from $(SYSTEM_SHAREDIR) (requires sudo)"
	@sudo rm -rf $(SYSTEM_SHAREDIR)
ifeq ($(OS),darwin)
	@echo ">> On macOS, manually remove from ~/.zshrc or ~/.bash_profile:"
	@echo ">>   export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"
else
	@echo ">> Removing $(PROFILE_FILE) (requires sudo)"
	@sudo rm -f $(PROFILE_FILE)
endif
	@echo ">> System installation removed"

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
