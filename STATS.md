# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 817 (Go: 709, C: 108)
- **Lines of code:** 186307 (Go: 157071, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 679 | 151677 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32523 |
| 2 | `internal/vm` | 23658 |
| 3 | `internal/backend/llvm` | 15940 |
| 4 | `internal/mir` | 12611 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8618 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 292
- **Lines of code:** 66482

## 📈 Total volume (code + tests)

- **Files:** 1109
- **Lines of code:** 252789

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 62%, C: 11%)
- **Tests:** 26%

