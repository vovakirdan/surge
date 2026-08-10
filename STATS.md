# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 981 (Go: 859, C: 122)
- **Lines of code:** 219076 (Go: 187612, C: 31464)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 827 | 182107 |
| `runtime/native/` (C code) | 122 | 31464 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43732 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 18464 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9313 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 482
- **Lines of code:** 103365

## 📈 Total volume (code + tests)

- **Files:** 1463
- **Lines of code:** 322441

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
