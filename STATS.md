# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1004 (Go: 878, C: 126)
- **Lines of code:** 224362 (Go: 192151, C: 32211)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 846 | 186646 |
| `runtime/native/` (C code) | 126 | 32211 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44801 |
| 2 | `internal/vm` | 27702 |
| 3 | `internal/backend/llvm` | 19616 |
| 4 | `internal/mir` | 16852 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 506
- **Lines of code:** 107848

## 📈 Total volume (code + tests)

- **Files:** 1510
- **Lines of code:** 332210

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
