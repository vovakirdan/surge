# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 664 (Go: 636, C: 28)
- **Lines of code:** 151642 (Go: 138693, C: 12949)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 26 | 4600 |
| `internal/` | 609 | 134078 |
| `runtime/native/` (C code) | 28 | 12949 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28094 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11394 |
| 4 | `internal/mir` | 9996 |
| 5 | `internal/parser` | 8932 |
| 6 | `internal/hir` | 7070 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4600 |

## 🧪 Test files

- **Files:** 153
- **Lines of code:** 32416

## 📈 Total volume (code + tests)

- **Files:** 817
- **Lines of code:** 184058

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

