# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1114 (Go: 953, C: 161)
- **Lines of code:** 243816 (Go: 204729, C: 39087)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 921 | 199756 |
| `runtime/native/` (C code) | 161 | 39087 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 48988 |
| 2 | `internal/vm` | 29899 |
| 3 | `internal/backend/llvm` | 21509 |
| 4 | `internal/mir` | 17961 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 668
- **Lines of code:** 137373

## 📈 Total volume (code + tests)

- **Files:** 1782
- **Lines of code:** 381189

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 53%, C: 10%)
- **Tests:** 36%
