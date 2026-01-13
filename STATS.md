# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 617 (Go: 591, C: 26)
- **Lines of code:** 138603 (Go: 126936, C: 11667)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 568 | 123219 |
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
| 7 | `internal/driver` | 5382 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 123
- **Lines of code:** 27257

## 📈 Total volume (code + tests)

- **Files:** 740
- **Lines of code:** 165860

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

