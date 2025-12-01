# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Build
```bash
make build          # Build surge binary
```

### Testing
```bash
make test           # Run all tests (timeout: 30s)
make golden-check   # Verify golden test files are current
make golden-update  # Update golden test files after changes
```

### Code Quality
```bash
make lint          # Run golangci-lint with comprehensive rules
make vet           # Run go vet static analysis
make staticcheck   # Run staticcheck analysis
make sec           # Run gosec security analysis
make check         # Run tests, lint, and file size checks
```

### Formatting
```bash
make fmt           # Format code with go fmt
```

### Running
```bash
make run [args]    # Run surge with arguments
./surge --help     # Show CLI help after building
```

### Profiling
```bash
make pprof-cpu     # Generate CPU profile and open web UI
make pprof-mem     # Generate memory profile and open web UI
make trace         # Generate execution trace
```

### Installation
```bash
make install        # Install to $GOBIN
make install-system # Install system-wide (requires sudo)
```

## Project Architecture

### Core Structure
- **cmd/surge/**: CLI entry point with subcommands (tokenize, parse, diag, fmt, fix, version, init, build)
- **internal/**: Core implementation modules
- **core/**: Surge language intrinsics and built-in types (intrinsics.sg, option.sg, result.sg)
- **stdlib/**: Standard library modules (bounded.sg, saturating_cast.sg)
- **testdata/**: Comprehensive test suite with golden files

### Key Internal Modules

#### Language Processing Pipeline
- **internal/lexer/**: Tokenization with trivia support and token limits
- **internal/parser/**: AST construction with generics, function parsing, contracts
- **internal/ast/**: Type-safe node arena, builder patterns, attribute catalog
- **internal/sema/**: Semantic analysis including type checking, symbol resolution, borrow checking

#### Analysis & Diagnostics
- **internal/diag/**: Diagnostic system with severity levels and error reporting
- **internal/diagfmt/**: Multiple output formats (JSON, pretty, SARIF, tokens)
- **internal/symbols/**: Symbol tables, name resolution, import handling

#### Development Tools
- **internal/format/**: Code formatting with comma handling and print utilities
- **internal/fix/**: Code fix suggestions and automated repairs
- **internal/driver/**: Project coordination, module caching, parallel processing

#### Infrastructure
- **internal/observ/**: Performance timing and observability
- **internal/fuzz/**: Fuzzing harnesses for lexer and parser
- **internal/version/**: Build information and version management

### Language Features

Surge is a systems programming language with:
- **Memory Safety**: Ownership/borrowing system similar to Rust
- **Type System**: Generics, contracts, tagged unions, extern types
- **Concurrency**: Async/await, spawn, signals with purity checking
- **Error Handling**: Option/Result types with exhaustive matching
- **Pattern Matching**: Compare expressions with exhaustiveness checking for tagged unions
- **Metaprogramming**: Pragma directives, attributes, compile-time features

### Testing Strategy

#### Golden Files
- Located in `testdata/golden/` with expected compiler outputs
- Use `make golden-update` after semantic analysis changes
- Organized by categories (sema/valid, sema/invalid, etc.)

#### Test Categories
- **Unit tests**: `*_test.go` files throughout internal/ modules
- **Integration tests**: Full pipeline tests in testdata/
- **Fuzz tests**: In internal/fuzz/ for lexer and parser robustness
- **Benchmark tests**: Performance testing for critical paths

### Configuration Files

- **.golangci.yaml**: Comprehensive linting with 20+ enabled rules
- **staticcheck.conf**: Static analysis configuration
- **Makefile**: Complete build/test automation
- **go.mod**: Go 1.25.1 with minimal external dependencies

### Development Workflow

1. **Make Changes**: Edit code in appropriate internal/ module
2. **Test**: Run `make test` for unit tests
3. **Update Golden Files**: Run `make golden-update` if semantic analysis changed
4. **Check Quality**: Run `make check` for comprehensive validation
5. **Format**: Run `make fmt` to ensure consistent style

### Common Development Tasks

#### Adding New Language Features
1. Extend lexer in `internal/lexer/` for new tokens
2. Update parser in `internal/parser/` for syntax
3. Add semantic analysis in `internal/sema/`
4. Update symbol resolution if needed
5. Add golden tests for both valid and invalid cases
6. Update intrinsics in `core/` if needed

#### Recent Semantic Analysis Enhancements
- **Exhaustiveness Checking**: Implemented in `internal/sema/type_expr_compare.go`
  - Validates complete pattern coverage in `compare` expressions for tagged unions
  - Supports wildcard patterns (`_`) and `finally` clauses
  - Diagnostic codes: `SemaNonexhaustiveMatch` (3053), `SemaRedundantFinally` (3054)
  - Currently disabled for stdlib files due to generic type handling complexity

#### Fixing Bugs
1. Add failing test case in appropriate testdata/ directory
2. Fix the implementation
3. Verify `make golden-check` passes
4. Run full `make check` before committing

#### Performance Optimization
1. Use profiling commands (`make pprof-cpu`, `make pprof-mem`)
2. Check internal/observ/ timing integration
3. Add benchmarks in `*_test.go` files
4. Verify no regressions in `make test`

### Important Notes

- **Never run commands with -i flag** (interactive mode not supported)
- **Always use sandbox=true** for Bash commands unless explicitly told otherwise
- **Prefer specialized tools**: Use Grep/Glob over bash grep/find commands
- **File size monitoring**: `check_file_sizes.sh` prevents binary bloat
- **Incremental compilation**: Module hashing and caching support planned but not yet implemented