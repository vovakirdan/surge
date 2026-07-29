# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 818 (Go: 710, C: 108)
- **Lines of code:** 187458 (Go: 158222, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 680 | 152828 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 33373 |
| 2 | `internal/vm` | 23735 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 12725 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8662 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 302
- **Lines of code:** 68504

## 📈 Total volume (code + tests)

- **Files:** 1120
- **Lines of code:** 255962

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 61%, C: 11%)
- **Tests:** 26%

