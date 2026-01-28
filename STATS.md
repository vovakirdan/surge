# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 646 (Go: 618, C: 28)
- **Lines of code:** 147832 (Go: 134888, C: 12944)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 595 | 131171 |
| `runtime/native/` (C code) | 28 | 12944 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26781 |
| 2 | `internal/vm` | 21845 |
| 3 | `internal/backend/llvm` | 11122 |
| 4 | `internal/mir` | 9822 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 5904 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 4630 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 140
- **Lines of code:** 29827

## 📈 Total volume (code + tests)

- **Files:** 786
- **Lines of code:** 177659

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 75%, C: 7%)
- **Tests:** 16%

