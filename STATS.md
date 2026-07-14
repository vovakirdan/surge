# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 797 (Go: 691, C: 106)
- **Lines of code:** 179303 (Go: 151376, C: 27927)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 661 | 145982 |
| `runtime/native/` (C code) | 106 | 27927 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30895 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 14279 |
| 4 | `internal/mir` | 11819 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7672 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5168 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 261
- **Lines of code:** 58892

## 📈 Total volume (code + tests)

- **Files:** 1058
- **Lines of code:** 238195

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

