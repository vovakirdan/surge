# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1014 (Go: 888, C: 126)
- **Lines of code:** 226973 (Go: 194490, C: 32483)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 856 | 188985 |
| `runtime/native/` (C code) | 126 | 32483 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45041 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20718 |
| 4 | `internal/mir` | 17041 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 516
- **Lines of code:** 110346

## 📈 Total volume (code + tests)

- **Files:** 1530
- **Lines of code:** 337319

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
