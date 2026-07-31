# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 819 (Go: 711, C: 108)
- **Lines of code:** 189280 (Go: 160044, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 681 | 154650 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34583 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 12979 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8862 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 310
- **Lines of code:** 70197

## 📈 Total volume (code + tests)

- **Files:** 1129
- **Lines of code:** 259477

## 📊 Percentage breakdown

- **Main code (Go + C):** 72% (Go: 61%, C: 11%)
- **Tests:** 27%

