# Makefile for Surge (C11, Linux/WSL2)
# Usage:
#   make                # build surge, surgec, surgetest (release)
#   make dev            # debug build with -O0 -g
#   make SAN=1          # enable address/ub sanitizers
#   make test           # run doctests if any Phase*/ present
#   make clean          # remove build artifacts
#   make distclean      # clean + remove .sbc and temp files
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
# Project layout
# -----------------------------
BUILD_DIR := build
OBJ_DIR   := $(BUILD_DIR)/obj
BIN_DIR   := $(BUILD_DIR)/bin
OUT_DIR   := $(BUILD_DIR)/out

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

# Default target
.PHONY: all
all: prep $(SURGE_BIN) $(SURGEC_BIN) $(SURGETEST_BIN)

# Debug build alias
.PHONY: dev
dev:
	$(MAKE) BUILD=dev all

# Prepare build folders
.PHONY: prep
prep:
	@$(MKDIR) $(BUILD_DIR) $(OBJ_DIR) $(BIN_DIR) $(OUT_DIR)
	@$(foreach d,$(SRC_DIRS) $(SURGE_DIR) $(SURGEC_DIR) $(SURGETEST_DIR), $(MKDIR) $(OBJ_DIR)/$(d);)

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

# Расширяем "test": после лексера — парсер
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

# Clean artifacts
.PHONY: clean
clean:
	$(RM) $(BUILD_DIR)

.PHONY: distclean
distclean: clean
	$(RM) **/*.sbc
	$(RM) tags

# Print variables (debug)
.PHONY: vars
vars:
	@echo "CC       = $(CC)"
	@echo "CFLAGS   = $(CFLAGS)"
	@echo "LDFLAGS  = $(LDFLAGS)"
	@echo "SRCS     = $(SRCS)"
	@echo "OBJS     = $(OBJS)"
