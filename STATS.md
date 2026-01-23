# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 641 (Go: 613, C: 28)
- **Lines of code:** 147088 (Go: 134208, C: 12880)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 590 | 130491 |
| `runtime/native/` (C code) | 28 | 12880 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26749 |
| 2 | `internal/vm` | 21777 |
| 3 | `internal/backend/llvm` | 11057 |
| 4 | `internal/mir` | 9534 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6970 |
| 7 | `internal/driver` | 5870 |
| 8 | `internal/lsp` | 4926 |
| 9 | `internal/mono` | 4613 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 139
- **Lines of code:** 29445

## 📈 Total volume (code + tests)

- **Files:** 780
- **Lines of code:** 176533

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

