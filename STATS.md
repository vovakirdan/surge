# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 626 (Go: 600, C: 26)
- **Lines of code:** 139889 (Go: 128222, C: 11667)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 577 | 124505 |
| `runtime/native/` (C code) | 26 | 11667 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26313 |
| 2 | `internal/vm` | 20718 |
| 3 | `internal/backend/llvm` | 10766 |
| 4 | `internal/mir` | 9305 |
| 5 | `internal/parser` | 8837 |
| 6 | `internal/hir` | 6818 |
| 7 | `internal/driver` | 5385 |
| 8 | `internal/mono` | 4613 |
| 9 | `internal/ast` | 4422 |
| 10 | `internal/diagfmt` | 4400 |

## 🧪 Test files

- **Files:** 127
- **Lines of code:** 27570

## 📈 Total volume (code + tests)

- **Files:** 753
- **Lines of code:** 167459

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 6%)
- **Tests:** 16%

