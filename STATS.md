# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 652 (Go: 624, C: 28)
- **Lines of code:** 149835 (Go: 136907, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4303 |
| `internal/` | 599 | 132589 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27074 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11317 |
| 4 | `internal/mir` | 9934 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 6002 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 5070 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 144
- **Lines of code:** 30421

## 📈 Total volume (code + tests)

- **Files:** 796
- **Lines of code:** 180256

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 75%, C: 7%)
- **Tests:** 16%

