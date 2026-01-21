# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 641 (Go: 613, C: 28)
- **Lines of code:** 146297 (Go: 133417, C: 12880)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 590 | 129700 |
| `runtime/native/` (C code) | 28 | 12880 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26565 |
| 2 | `internal/vm` | 21615 |
| 3 | `internal/backend/llvm` | 11003 |
| 4 | `internal/mir` | 9534 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6875 |
| 7 | `internal/driver` | 5870 |
| 8 | `internal/lsp` | 4630 |
| 9 | `internal/mono` | 4613 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 136
- **Lines of code:** 28938

## 📈 Total volume (code + tests)

- **Files:** 777
- **Lines of code:** 175235

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

