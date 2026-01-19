# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 632 (Go: 604, C: 28)
- **Lines of code:** 142824 (Go: 130635, C: 12189)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 581 | 126918 |
| `runtime/native/` (C code) | 28 | 12189 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26536 |
| 2 | `internal/vm` | 21615 |
| 3 | `internal/backend/llvm` | 10891 |
| 4 | `internal/mir` | 9442 |
| 5 | `internal/parser` | 8878 |
| 6 | `internal/hir` | 6875 |
| 7 | `internal/driver` | 5839 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 132
- **Lines of code:** 28532

## 📈 Total volume (code + tests)

- **Files:** 764
- **Lines of code:** 171356

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

