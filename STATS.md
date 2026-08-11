# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 987 (Go: 863, C: 124)
- **Lines of code:** 220912 (Go: 188899, C: 32013)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 831 | 183394 |
| `runtime/native/` (C code) | 124 | 32013 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43918 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19470 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/hir` | 9386 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 483
- **Lines of code:** 103547

## 📈 Total volume (code + tests)

- **Files:** 1470
- **Lines of code:** 324459

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 9%)
- **Tests:** 31%
