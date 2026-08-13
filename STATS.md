# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 999 (Go: 873, C: 126)
- **Lines of code:** 223310 (Go: 191150, C: 32160)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 841 | 185645 |
| `runtime/native/` (C code) | 126 | 32160 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44294 |
| 2 | `internal/vm` | 27421 |
| 3 | `internal/backend/llvm` | 19586 |
| 4 | `internal/mir` | 16839 |
| 5 | `internal/hir` | 9386 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 496
- **Lines of code:** 106471

## 📈 Total volume (code + tests)

- **Files:** 1495
- **Lines of code:** 329781

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
