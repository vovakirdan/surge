# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 830 (Go: 722, C: 108)
- **Lines of code:** 192151 (Go: 162823, C: 29328)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4836 |
| `internal/` | 693 | 157972 |
| `runtime/native/` (C code) | 108 | 29328 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34744 |
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

- **Files:** 353
- **Lines of code:** 80212

## 📈 Total volume (code + tests)

- **Files:** 1183
- **Lines of code:** 272363

## 📊 Percentage breakdown

- **Main code (Go + C):** 70% (Go: 59%, C: 10%)
- **Tests:** 29%

