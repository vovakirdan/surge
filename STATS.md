# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 661 (Go: 633, C: 28)
- **Lines of code:** 150739 (Go: 137811, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4303 |
| `internal/` | 608 | 133493 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27579 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11369 |
| 4 | `internal/mir` | 9989 |
| 5 | `internal/parser` | 8932 |
| 6 | `internal/hir` | 7053 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `internal/ast` | 4448 |

## 🧪 Test files

- **Files:** 148
- **Lines of code:** 31478

## 📈 Total volume (code + tests)

- **Files:** 809
- **Lines of code:** 182217

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

