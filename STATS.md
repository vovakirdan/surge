# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 651 (Go: 623, C: 28)
- **Lines of code:** 149647 (Go: 136703, C: 12944)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4291 |
| `internal/` | 598 | 132397 |
| `runtime/native/` (C code) | 28 | 12944 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26841 |
| 2 | `internal/vm` | 21887 |
| 3 | `internal/backend/llvm` | 11319 |
| 4 | `internal/mir` | 9934 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 6055 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 5070 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 142
- **Lines of code:** 29927

## 📈 Total volume (code + tests)

- **Files:** 793
- **Lines of code:** 179574

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

