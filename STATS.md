# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 651 (Go: 623, C: 28)
- **Lines of code:** 149583 (Go: 136655, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4291 |
| `internal/` | 598 | 132349 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26841 |
| 2 | `internal/vm` | 21887 |
| 3 | `internal/backend/llvm` | 11317 |
| 4 | `internal/mir` | 9934 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 5070 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 143
- **Lines of code:** 30191

## 📈 Total volume (code + tests)

- **Files:** 794
- **Lines of code:** 179774

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

