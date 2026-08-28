# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1064 (Go: 917, C: 147)
- **Lines of code:** 236218 (Go: 198822, C: 37396)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 884 | 193289 |
| `runtime/native/` (C code) | 147 | 37396 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47097 |
| 2 | `internal/vm` | 28584 |
| 3 | `internal/backend/llvm` | 21093 |
| 4 | `internal/mir` | 17192 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9485 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 599
- **Lines of code:** 123447

## 📈 Total volume (code + tests)

- **Files:** 1663
- **Lines of code:** 359665

## 📊 Percentage breakdown

- **Main code (Go + C):** 65% (Go: 55%, C: 10%)
- **Tests:** 34%
