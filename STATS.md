# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1003 (Go: 877, C: 126)
- **Lines of code:** 223961 (Go: 191750, C: 32211)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 845 | 186245 |
| `runtime/native/` (C code) | 126 | 32211 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44801 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19616 |
| 4 | `internal/mir` | 16852 |
| 5 | `internal/hir` | 9392 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 503
- **Lines of code:** 107163

## 📈 Total volume (code + tests)

- **Files:** 1506
- **Lines of code:** 331124

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
