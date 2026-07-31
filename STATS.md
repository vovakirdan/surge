# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 819 (Go: 711, C: 108)
- **Lines of code:** 189374 (Go: 160138, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 681 | 154744 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34653 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 13003 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8862 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 312
- **Lines of code:** 70609

## 📈 Total volume (code + tests)

- **Files:** 1131
- **Lines of code:** 259983

## 📊 Percentage breakdown

- **Main code (Go + C):** 72% (Go: 61%, C: 11%)
- **Tests:** 27%

