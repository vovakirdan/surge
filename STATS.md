# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 607 (Go: 581, C: 26)
- **Lines of code:** 137389 (Go: 125722, C: 11667)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 20 | 3597 |
| `internal/` | 560 | 122110 |
| `runtime/native/` (C code) | 26 | 11667 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26313 |
| 2 | `internal/vm` | 20901 |
| 3 | `internal/backend/llvm` | 10766 |
| 4 | `internal/mir` | 9305 |
| 5 | `internal/parser` | 8837 |
| 6 | `internal/hir` | 6818 |
| 7 | `internal/driver` | 5369 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 119
- **Lines of code:** 27009

## 📈 Total volume (code + tests)

- **Files:** 726
- **Lines of code:** 164398

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

