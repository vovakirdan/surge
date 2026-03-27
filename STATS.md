# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 659 (Go: 631, C: 28)
- **Lines of code:** 150567 (Go: 137639, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4303 |
| `internal/` | 606 | 133321 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27407 |
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
- **Lines of code:** 31219

## 📈 Total volume (code + tests)

- **Files:** 807
- **Lines of code:** 181786

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

