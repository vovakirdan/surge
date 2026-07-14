# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 794 (Go: 688, C: 106)
- **Lines of code:** 178725 (Go: 150798, C: 27927)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 658 | 145404 |
| `runtime/native/` (C code) | 106 | 27927 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30530 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 14279 |
| 4 | `internal/mir` | 11774 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7519 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 258
- **Lines of code:** 58278

## 📈 Total volume (code + tests)

- **Files:** 1052
- **Lines of code:** 237003

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

