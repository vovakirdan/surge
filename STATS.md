# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1100 (Go: 945, C: 155)
- **Lines of code:** 241203 (Go: 202293, C: 38910)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 913 | 197320 |
| `runtime/native/` (C code) | 155 | 38910 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47165 |
| 2 | `internal/vm` | 29899 |
| 3 | `internal/backend/llvm` | 21473 |
| 4 | `internal/mir` | 17426 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 649
- **Lines of code:** 134209

## 📈 Total volume (code + tests)

- **Files:** 1749
- **Lines of code:** 375412

## 📊 Percentage breakdown

- **Main code (Go + C):** 64% (Go: 53%, C: 10%)
- **Tests:** 35%
