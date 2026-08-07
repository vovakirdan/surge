# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 963 (Go: 841, C: 122)
- **Lines of code:** 214700 (Go: 183450, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 810 | 178505 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 26974 |
| 3 | `internal/mir` | 16585 |
| 4 | `internal/backend/llvm` | 16265 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 470
- **Lines of code:** 101278

## 📈 Total volume (code + tests)

- **Files:** 1433
- **Lines of code:** 315978

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
