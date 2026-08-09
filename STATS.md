# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 980 (Go: 858, C: 122)
- **Lines of code:** 218034 (Go: 186636, C: 31398)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 826 | 181131 |
| `runtime/native/` (C code) | 122 | 31398 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43553 |
| 2 | `internal/vm` | 27049 |
| 3 | `internal/backend/llvm` | 18214 |
| 4 | `internal/mir` | 16718 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7657 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 480
- **Lines of code:** 103813

## 📈 Total volume (code + tests)

- **Files:** 1460
- **Lines of code:** 321847

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
