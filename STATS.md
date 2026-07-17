# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 806 (Go: 698, C: 108)
- **Lines of code:** 182499 (Go: 153860, C: 28639)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 668 | 148466 |
| `runtime/native/` (C code) | 108 | 28639 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32041 |
| 2 | `internal/vm` | 23510 |
| 3 | `internal/backend/llvm` | 14993 |
| 4 | `internal/mir` | 11998 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7848 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 270
- **Lines of code:** 60355

## 📈 Total volume (code + tests)

- **Files:** 1076
- **Lines of code:** 242854

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

