# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 806 (Go: 698, C: 108)
- **Lines of code:** 182427 (Go: 153806, C: 28621)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 668 | 148412 |
| `runtime/native/` (C code) | 108 | 28621 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32041 |
| 2 | `internal/vm` | 23510 |
| 3 | `internal/backend/llvm` | 14939 |
| 4 | `internal/mir` | 11998 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7848 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 269
- **Lines of code:** 60202

## 📈 Total volume (code + tests)

- **Files:** 1075
- **Lines of code:** 242629

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

