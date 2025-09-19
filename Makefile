# Makefile for Surge (C11, Linux/WSL2)
# Usage:
#   make                # build surge, surgec, surgetest (release) -> build/bin/
#   make dev            # debug build with -O0 -g -> build/bin/v{VERSION}/
#   make SAN=1          # enable address/ub sanitizers
#   make test           # run doctests if any Phase*/ present
#   make make-hello-sbc # create sample.sbc using tools/make_hello_sbc.c
#   make clean          # remove current dev version (v{VERSION}) + obj/
#   make clean-all      # remove all build artifacts (entire build/)
#   make clean-release  # remove only release binaries + obj/
#   make distclean      # clean-all + remove .sbc and temp files
#   make version        # show current version from config.h
#   make vars           # show build variables
#
# Structure assumptions (can be extended later):
#   cmd/surge/main.c
#   cmd/surgec/main.c
#   cmd/surgetest/main.c
#   src/**.c
#   include/**.h
#
# Phase directories:
#   You can add PhaseA/, PhaseB/, ... with sample .sg programs.
#   `make test` will try to run surgetest for each .sg file it finds.

# -----------------------------
# Toolchain and flags
# -----------------------------
CC       ?= gcc
CSTD     ?= -std=c11
WARN     := -Wall -Wextra -Werror -Wpedantic -Wshadow -Wpointer-arith -Wcast-align -Wstrict-prototypes
OPT_REL  := -O2
OPT_DEV  := -O0 -g3
DEFS     := -D_GNU_SOURCE
THREAD   := -pthread
INC      := -Iinclude

# Sanitizers (opt-in with SAN=1)
ifeq ($(SAN),1)
SANFLAGS := -fsanitize=address,undefined -fno-omit-frame-pointer
else
SANFLAGS :=
endif

# Build mode (default = release). Use: `make dev` for debug.
BUILD    ?= release
ifeq ($(BUILD),release)
CFLAGS   := $(CSTD) $(WARN) $(OPT_REL) $(DEFS) $(THREAD) $(INC) $(SANFLAGS)
else
CFLAGS   := $(CSTD) $(WARN) $(OPT_DEV) $(DEFS) $(THREAD) $(INC) $(SANFLAGS)
endif
LDFLAGS  := $(THREAD) $(SANFLAGS)
AR       := ar
RM       := rm -rf
MKDIR    := mkdir -p
FIND     := find

# -----------------------------
# Version extraction from config.h
# -----------------------------
# Извлекаем версию из include/config.h
VERSION_MAJOR := $(shell grep 'SURGE_VERSION_MAJOR' include/config.h | awk '{print $$3}')
VERSION_MINOR := $(shell grep 'SURGE_VERSION_MINOR' include/config.h | awk '{print $$3}')
VERSION_PATCH := $(shell grep 'SURGE_VERSION_PATCH' include/config.h | awk '{print $$3}')
VERSION_STR   := $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)

# -----------------------------
# Project layout
# -----------------------------
BUILD_DIR := build

# Для dev-сборки создаем версионную поддиректорию
ifeq ($(BUILD),dev)
BIN_DIR   := $(BUILD_DIR)/bin/v$(VERSION_STR)
OUT_DIR   := $(BUILD_DIR)/out/v$(VERSION_STR)
else
BIN_DIR   := $(BUILD_DIR)/bin
OUT_DIR   := $(BUILD_DIR)/out
endif

OBJ_DIR   := $(BUILD_DIR)/obj

# Collect sources (core/front/back/runtime/testing/extras)
SRC_DIRS := src src/core src/front src/back src/runtime src/testing src/extras
SRCS     := $(foreach d,$(SRC_DIRS),$(wildcard $(d)/*.c))
OBJS     := $(patsubst %.c,$(OBJ_DIR)/%.o,$(SRCS))

# Commands (binaries)
SURGE_DIR    := cmd/surge
SURGEC_DIR   := cmd/surgec
SURGETEST_DIR:= cmd/surgetest

SURGE_SRC    := $(wildcard $(SURGE_DIR)/*.c)
SURGEC_SRC   := $(wildcard $(SURGEC_DIR)/*.c)
SURGETEST_SRC:= $(wildcard $(SURGETEST_DIR)/*.c)

SURGE_OBJ    := $(patsubst %.c,$(OBJ_DIR)/%.o,$(SURGE_SRC))
SURGEC_OBJ   := $(patsubst %.c,$(OBJ_DIR)/%.o,$(SURGEC_SRC))
SURGETEST_OBJ:= $(patsubst %.c,$(OBJ_DIR)/%.o,$(SURGETEST_SRC))

SURGE_BIN     := $(BIN_DIR)/surge
SURGEC_BIN    := $(BIN_DIR)/surgec
SURGETEST_BIN := $(BIN_DIR)/surgetest

# Tools
SAMPLE_SBC_SRC := tools/make_hello_sbc.c
SAMPLE_SBC_OBJ := $(patsubst %.c,$(OBJ_DIR)/%.o,$(SAMPLE_SBC_SRC))
SAMPLE_SBC_BIN := $(BIN_DIR)/sample_sbc

# Default target
.PHONY: all
all: prep $(SURGE_BIN) $(SURGEC_BIN) $(SURGETEST_BIN)

# Debug build alias (создает версионную поддиректорию)
.PHONY: dev
dev:
	@echo "[surge] Building dev version $(VERSION_STR) -> $(BIN_DIR)"
	$(MAKE) BUILD=dev all

# Prepare build folders
.PHONY: prep
prep:
	@$(MKDIR) $(BUILD_DIR) $(OBJ_DIR) $(BIN_DIR) $(OUT_DIR)
	@$(foreach d,$(SRC_DIRS) $(SURGE_DIR) $(SURGEC_DIR) $(SURGETEST_DIR), $(MKDIR) $(OBJ_DIR)/$(d);)
	@$(MKDIR) $(OBJ_DIR)/tools

# Pattern rule for object files
$(OBJ_DIR)/%.o: %.c
	@$(MKDIR) $(dir $@)
	$(CC) $(CFLAGS) -c $< -o $@

# Link binaries
$(SURGE_BIN): $(OBJS) $(SURGE_OBJ)
	$(CC) $(CFLAGS) $(OBJS) $(SURGE_OBJ) -o $@ $(LDFLAGS)

$(SURGEC_BIN): $(OBJS) $(SURGEC_OBJ)
	$(CC) $(CFLAGS) $(OBJS) $(SURGEC_OBJ) -o $@ $(LDFLAGS)

$(SURGETEST_BIN): $(OBJS) $(SURGETEST_OBJ)
	$(CC) $(CFLAGS) $(OBJS) $(SURGETEST_OBJ) -o $@ $(LDFLAGS)

$(SAMPLE_SBC_BIN): $(OBJS) $(SAMPLE_SBC_OBJ)
	$(CC) $(CFLAGS) $(OBJS) $(SAMPLE_SBC_OBJ) -o $@ $(LDFLAGS)

# -----------------------------
# Convenience targets
# -----------------------------

# -----------------------------
# Lexer golden tests
# -----------------------------
.PHONY: lex-golden-update
lex-golden-update: all
	@bash tools/lex_golden.sh update

.PHONY: lex-golden
lex-golden: all
	@bash tools/lex_golden.sh check

# -----------------------------
# Parser golden tests
# -----------------------------
.PHONY: parse-golden-update
parse-golden-update: all
	@bash tools/parse_golden.sh update

.PHONY: parse-golden
parse-golden: all
	@bash tools/parse_golden.sh check

# -----------------------------
# Diagnostics golden tests
# -----------------------------
.PHONY: diag-golden-update
diag-golden-update: all
	@bash tools/diag_golden.sh update

.PHONY: diag-golden
diag-golden: all
	@bash tools/diag_golden.sh check

# -----------------------------
# Sema golden tests
# -----------------------------
.PHONY: sema-golden-update
sema-golden-update: all
	@bash tools/sema_golden.sh update

.PHONY: sema-golden
sema-golden: all
	@bash tools/sema_golden.sh check

# -----------------------------
# Disassembler golden tests
# -----------------------------
.PHONY: disasm-golden-update
disasm-golden-update: all
	@bash tools/disasm_golden.sh update

.PHONY: disasm-golden
disasm-golden: all
	@bash tools/disasm_golden.sh check

# -----------------------------
# VM golden tests
# -----------------------------
.PHONY: vm-golden-update
vm-golden-update: all
	@bash tools/vm_golden.sh update

.PHONY: vm-golden
vm-golden: all
	@bash tools/vm_golden.sh check

# В общий тест-ран:
test: all
	@set -e; \
	if [ -x "$(SURGETEST_BIN)" ]; then \
	  if ls Phase*/ 1>/dev/null 2>&1; then \
	    echo "[surge] Running doctests in Phase* ..."; \
	    for d in $$(ls -d Phase*/ 2>/dev/null); do \
	      for f in $${d}*.sg; do \
	        [ -f "$$f" ] || continue; \
	        echo "  -> $$f"; \
	        "$(SURGETEST_BIN)" "$$f" || true; \
	      done; \
	    done; \
	  else \
	    echo "[surge] No Phase* directories found. Skipping doctests."; \
	  fi; \
	else \
	  echo "[surge] surgetest binary missing? (stub ok)"; \
	fi
	@$(MAKE) lex-golden
	@$(MAKE) parse-golden
	@$(MAKE) diag-golden
	@$(MAKE) sema-golden
	@$(MAKE) disasm-golden
	@$(MAKE) vm-golden

# Example: compile a single .sg to .sbc (if surgec is implemented)
# Usage: make compile FILE=examples/hello.sg
.PHONY: compile
compile: all
	@if [ -z "$(FILE)" ]; then echo "Usage: make compile FILE=path/to/file.sg"; exit 2; fi
	@$(MKDIR) $(OUT_DIR)
	$(SURGEC_BIN) "$(FILE)" -o "$(OUT_DIR)/$$(basename "$(FILE)" .sg).sbc"

# Example: run a .sg directly via surge (JIT/interpret in-memory)
# Usage: make run FILE=examples/hello.sg
.PHONY: run
run: all
	@if [ -z "$(FILE)" ]; then echo "Usage: make run FILE=path/to/file.sg"; exit 2; fi
	$(SURGE_BIN) "$(FILE)"

# Example: run a compiled .sbc
# Usage: make runbc FILE=build/out/hello.sbc
.PHONY: runbc
runbc: all
	@if [ -z "$(FILE)" ]; then echo "Usage: make runbc FILE=path/to/file.sbc"; exit 2; fi
	$(SURGE_BIN) "$(FILE)"

# Create sample.sbc using make_hello_sbc tool
.PHONY: make-hello-sbc
make-hello-sbc: prep $(SAMPLE_SBC_BIN)
	@$(MKDIR) $(OUT_DIR)
	$(SAMPLE_SBC_BIN) "$(OUT_DIR)/sample.sbc"

# Clean artifacts
.PHONY: clean
clean:
	@echo "[surge] Cleaning dev version $(VERSION_STR)..."
	$(RM) $(BUILD_DIR)/bin/v$(VERSION_STR)
	$(RM) $(BUILD_DIR)/out/v$(VERSION_STR)
	$(RM) $(BUILD_DIR)/obj
	@echo "[surge] Dev version $(VERSION_STR) cleaned."

.PHONY: clean-all
clean-all:
	@echo "[surge] Cleaning all build artifacts..."
	$(RM) $(BUILD_DIR)
	@echo "[surge] All build artifacts cleaned."

.PHONY: clean-release
clean-release:
	@echo "[surge] Cleaning release build..."
	$(RM) $(BUILD_DIR)/bin/surge $(BUILD_DIR)/bin/surgec $(BUILD_DIR)/bin/surgetest $(BUILD_DIR)/bin/sample_sbc
	$(RM) $(BUILD_DIR)/out/*.sbc 2>/dev/null || true
	$(RM) $(BUILD_DIR)/obj
	@echo "[surge] Release build cleaned."

.PHONY: distclean
distclean: clean-all
	@echo "[surge] Deep cleaning..."
	$(FIND) . -name "*.sbc" -type f -delete 2>/dev/null || true
	$(RM) tags
	@echo "[surge] Deep clean completed."

# Print variables (debug)
.PHONY: vars
vars:
	@echo "VERSION  = $(VERSION_STR)"
	@echo "BUILD    = $(BUILD)"
	@echo "CC       = $(CC)"
	@echo "CFLAGS   = $(CFLAGS)"
	@echo "LDFLAGS  = $(LDFLAGS)"
	@echo "BIN_DIR  = $(BIN_DIR)"
	@echo "OUT_DIR  = $(OUT_DIR)"
	@echo "SRCS     = $(SRCS)"
	@echo "OBJS     = $(OBJS)"

# Show current version from config.h
.PHONY: version
version:
	@echo "Surge version: $(VERSION_STR)"
