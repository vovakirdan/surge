# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 631 (Go: 604, C: 27)
- **Lines of code:** 142452 (Go: 130243, C: 12209)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 581 | 126526 |
| `runtime/native/` (C code) | 27 | 12209 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26349 |
| 2 | `internal/vm` | 21615 |
| 3 | `internal/backend/llvm` | 10891 |
| 4 | `internal/mir` | 9422 |
| 5 | `internal/parser` | 8854 |
| 6 | `internal/hir` | 6851 |
| 7 | `internal/driver` | 5816 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 129
- **Lines of code:** 28220

## 📈 Total volume (code + tests)

- **Files:** 760
- **Lines of code:** 170672

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

