# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 605 (Go: 580, C: 25)
- **Lines of code:** 136318 (Go: 124968, C: 11350)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 20 | 3589 |
| `internal/` | 559 | 121364 |
| `runtime/native/` (C code) | 25 | 11350 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26244 |
| 2 | `internal/vm` | 20844 |
| 3 | `internal/backend/llvm` | 10160 |
| 4 | `internal/mir` | 9305 |
| 5 | `internal/parser` | 8837 |
| 6 | `internal/hir` | 6818 |
| 7 | `internal/driver` | 5355 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 118
- **Lines of code:** 27221

## 📈 Total volume (code + tests)

- **Files:** 723
- **Lines of code:** 163539

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 6%)
- **Tests:** 16%

