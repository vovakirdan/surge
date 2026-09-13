# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1168 (Go: 987, C: 181)
- **Lines of code:** 254491 (Go: 210756, C: 43735)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 955 | 205783 |
| `runtime/native/` (C code) | 181 | 43735 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51217 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19111 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9504 |
| 7 | `internal/driver` | 7708 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 811
- **Lines of code:** 164809

## 📈 Total volume (code + tests)

- **Files:** 1979
- **Lines of code:** 419300

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
