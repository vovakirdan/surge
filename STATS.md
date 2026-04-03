# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 669 (Go: 641, C: 28)
- **Lines of code:** 151749 (Go: 138800, C: 12949)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4635 |
| `internal/` | 613 | 134150 |
| `runtime/native/` (C code) | 28 | 12949 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28111 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11394 |
| 4 | `internal/mir` | 10023 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7070 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4635 |

## 🧪 Test files

- **Files:** 155
- **Lines of code:** 32801

## 📈 Total volume (code + tests)

- **Files:** 824
- **Lines of code:** 184550

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

