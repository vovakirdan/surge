# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 829 (Go: 721, C: 108)
- **Lines of code:** 192412 (Go: 163134, C: 29278)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 691 | 157740 |
| `runtime/native/` (C code) | 108 | 29278 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34744 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 16065 |
| 4 | `internal/mir` | 15065 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 346
- **Lines of code:** 78494

## 📈 Total volume (code + tests)

- **Files:** 1175
- **Lines of code:** 270906

## 📊 Percentage breakdown

- **Main code (Go + C):** 71% (Go: 60%, C: 10%)
- **Tests:** 28%

