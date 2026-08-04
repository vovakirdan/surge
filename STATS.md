# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 831 (Go: 723, C: 108)
- **Lines of code:** 192258 (Go: 162892, C: 29366)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4836 |
| `internal/` | 694 | 158041 |
| `runtime/native/` (C code) | 108 | 29366 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34813 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 16065 |
| 4 | `internal/mir` | 15243 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4836 |

## 🧪 Test files

- **Files:** 358
- **Lines of code:** 81110

## 📈 Total volume (code + tests)

- **Files:** 1189
- **Lines of code:** 273368

## 📊 Percentage breakdown

- **Main code (Go + C):** 70% (Go: 59%, C: 10%)
- **Tests:** 29%

