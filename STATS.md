# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1172 (Go: 990, C: 182)
- **Lines of code:** 254799 (Go: 211054, C: 43745)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 958 | 206081 |
| `runtime/native/` (C code) | 182 | 43745 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51201 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 23050 |
| 4 | `internal/mir` | 19255 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9508 |
| 7 | `internal/driver` | 7708 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 818
- **Lines of code:** 166039

## 📈 Total volume (code + tests)

- **Files:** 1990
- **Lines of code:** 420838

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
