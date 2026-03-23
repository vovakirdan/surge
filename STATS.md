# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 658 (Go: 630, C: 28)
- **Lines of code:** 150036 (Go: 137108, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4303 |
| `internal/` | 605 | 132790 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27223 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11369 |
| 4 | `internal/mir` | 9934 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 6002 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 5070 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 147
- **Lines of code:** 30729

## 📈 Total volume (code + tests)

- **Files:** 805
- **Lines of code:** 180765

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 75%, C: 7%)
- **Tests:** 16%

