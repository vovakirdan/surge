# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 975 (Go: 853, C: 122)
- **Lines of code:** 217004 (Go: 185754, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 821 | 180249 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 26974 |
| 3 | `internal/backend/llvm` | 17804 |
| 4 | `internal/mir` | 16705 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 475
- **Lines of code:** 102824

## 📈 Total volume (code + tests)

- **Files:** 1450
- **Lines of code:** 319828

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
