# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 664 (Go: 636, C: 28)
- **Lines of code:** 151351 (Go: 138402, C: 12949)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 26 | 4600 |
| `internal/` | 609 | 133787 |
| `runtime/native/` (C code) | 28 | 12949 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27889 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11361 |
| 4 | `internal/mir` | 9989 |
| 5 | `internal/parser` | 8932 |
| 6 | `internal/hir` | 7053 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4600 |

## 🧪 Test files

- **Files:** 152
- **Lines of code:** 32293

## 📈 Total volume (code + tests)

- **Files:** 816
- **Lines of code:** 183644

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

