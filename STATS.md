# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 996 (Go: 870, C: 126)
- **Lines of code:** 222746 (Go: 190630, C: 32116)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 838 | 185125 |
| `runtime/native/` (C code) | 126 | 32116 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43940 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19540 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/hir` | 9386 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 488
- **Lines of code:** 105596

## 📈 Total volume (code + tests)

- **Files:** 1484
- **Lines of code:** 328342

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
