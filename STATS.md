# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 994 (Go: 870, C: 124)
- **Lines of code:** 222523 (Go: 190500, C: 32023)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 838 | 184995 |
| `runtime/native/` (C code) | 124 | 32023 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43918 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19480 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/hir` | 9386 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 485
- **Lines of code:** 103949

## 📈 Total volume (code + tests)

- **Files:** 1479
- **Lines of code:** 326472

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 9%)
- **Tests:** 31%
