# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 818 (Go: 710, C: 108)
- **Lines of code:** 188210 (Go: 158974, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 680 | 153580 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 33990 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 12769 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8693 |
| 7 | `internal/driver` | 6389 |
| 8 | `internal/mono` | 5298 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 304
- **Lines of code:** 69409

## 📈 Total volume (code + tests)

- **Files:** 1122
- **Lines of code:** 257619

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 61%, C: 11%)
- **Tests:** 26%

