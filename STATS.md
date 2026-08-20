# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1013 (Go: 887, C: 126)
- **Lines of code:** 226279 (Go: 193830, C: 32449)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 855 | 188325 |
| `runtime/native/` (C code) | 126 | 32449 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45041 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20233 |
| 4 | `internal/mir` | 16866 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 515
- **Lines of code:** 110170

## 📈 Total volume (code + tests)

- **Files:** 1528
- **Lines of code:** 336449

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
