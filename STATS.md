# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 851 (Go: 743, C: 108)
- **Lines of code:** 196057 (Go: 166691, C: 29366)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 29 | 4888 |
| `internal/` | 713 | 161788 |
| `runtime/native/` (C code) | 108 | 29366 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 35400 |
| 2 | `internal/vm` | 23806 |
| 3 | `internal/backend/llvm` | 16056 |
| 4 | `internal/mir` | 15789 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6436 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 384
- **Lines of code:** 84556

## 📈 Total volume (code + tests)

- **Files:** 1235
- **Lines of code:** 280613

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 59%, C: 10%)
- **Tests:** 30%

