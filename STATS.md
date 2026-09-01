# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1101 (Go: 945, C: 156)
- **Lines of code:** 241333 (Go: 202331, C: 39002)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 913 | 197358 |
| `runtime/native/` (C code) | 156 | 39002 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47165 |
| 2 | `internal/vm` | 29899 |
| 3 | `internal/backend/llvm` | 21474 |
| 4 | `internal/mir` | 17426 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 651
- **Lines of code:** 134680

## 📈 Total volume (code + tests)

- **Files:** 1752
- **Lines of code:** 376013

## 📊 Percentage breakdown

- **Main code (Go + C):** 64% (Go: 53%, C: 10%)
- **Tests:** 35%
