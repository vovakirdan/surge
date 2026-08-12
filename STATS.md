# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 996 (Go: 870, C: 126)
- **Lines of code:** 222990 (Go: 190830, C: 32160)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 838 | 185325 |
| `runtime/native/` (C code) | 126 | 32160 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44072 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19586 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/hir` | 9386 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 491
- **Lines of code:** 106151

## 📈 Total volume (code + tests)

- **Files:** 1487
- **Lines of code:** 329141

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
