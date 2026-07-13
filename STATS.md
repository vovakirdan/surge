# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 792 (Go: 688, C: 104)
- **Lines of code:** 178300 (Go: 150726, C: 27574)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 658 | 145332 |
| `runtime/native/` (C code) | 104 | 27574 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30525 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 14204 |
| 4 | `internal/mir` | 11774 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7519 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 255
- **Lines of code:** 57804

## 📈 Total volume (code + tests)

- **Files:** 1047
- **Lines of code:** 236104

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

