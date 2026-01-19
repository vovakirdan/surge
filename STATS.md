# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 632 (Go: 604, C: 28)
- **Lines of code:** 142673 (Go: 130484, C: 12189)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 581 | 126767 |
| `runtime/native/` (C code) | 28 | 12189 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26536 |
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

- **Files:** 131
- **Lines of code:** 28271

## 📈 Total volume (code + tests)

- **Files:** 763
- **Lines of code:** 170944

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

