# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 974 (Go: 852, C: 122)
- **Lines of code:** 216381 (Go: 185131, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 821 | 180186 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 26974 |
| 3 | `internal/backend/llvm` | 17741 |
| 4 | `internal/mir` | 16705 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 475
- **Lines of code:** 102824

## 📈 Total volume (code + tests)

- **Files:** 1449
- **Lines of code:** 319205

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
