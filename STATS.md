# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 818 (Go: 710, C: 108)
- **Lines of code:** 188737 (Go: 159501, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 680 | 154107 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34431 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 12769 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8753 |
| 7 | `internal/driver` | 6389 |
| 8 | `internal/mono` | 5298 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 308
- **Lines of code:** 69890

## 📈 Total volume (code + tests)

- **Files:** 1126
- **Lines of code:** 258627

## 📊 Percentage breakdown

- **Main code (Go + C):** 72% (Go: 61%, C: 11%)
- **Tests:** 27%

