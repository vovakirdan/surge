# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 985 (Go: 862, C: 123)
- **Lines of code:** 219750 (Go: 188124, C: 31626)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 830 | 182619 |
| `runtime/native/` (C code) | 123 | 31626 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43732 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 18976 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9313 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 482
- **Lines of code:** 103374

## 📈 Total volume (code + tests)

- **Files:** 1467
- **Lines of code:** 323124

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 9%)
- **Tests:** 31%
