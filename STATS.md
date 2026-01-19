# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 632 (Go: 604, C: 28)
- **Lines of code:** 142638 (Go: 130449, C: 12189)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 581 | 126732 |
| `runtime/native/` (C code) | 28 | 12189 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26501 |
| 2 | `internal/vm` | 21615 |
| 3 | `internal/backend/llvm` | 10891 |
| 4 | `internal/mir` | 9428 |
| 5 | `internal/parser` | 8878 |
| 6 | `internal/hir` | 6875 |
| 7 | `internal/driver` | 5816 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 130
- **Lines of code:** 28244

## 📈 Total volume (code + tests)

- **Files:** 762
- **Lines of code:** 170882

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

