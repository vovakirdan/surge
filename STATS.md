# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 871 (Go: 756, C: 115)
- **Lines of code:** 198401 (Go: 168188, C: 30213)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 725 | 163249 |
| `runtime/native/` (C code) | 115 | 30213 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 35400 |
| 2 | `internal/vm` | 23813 |
| 3 | `internal/backend/llvm` | 16167 |
| 4 | `internal/mir` | 15789 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6436 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 391
- **Lines of code:** 86059

## 📈 Total volume (code + tests)

- **Files:** 1262
- **Lines of code:** 284460

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 59%, C: 10%)
- **Tests:** 30%

