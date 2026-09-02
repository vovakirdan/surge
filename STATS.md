# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1105 (Go: 946, C: 159)
- **Lines of code:** 241997 (Go: 202788, C: 39209)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 914 | 197815 |
| `runtime/native/` (C code) | 159 | 39209 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47622 |
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

- **Files:** 663
- **Lines of code:** 136674

## 📈 Total volume (code + tests)

- **Files:** 1768
- **Lines of code:** 378671

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 53%, C: 10%)
- **Tests:** 36%
