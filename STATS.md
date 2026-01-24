# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 641 (Go: 613, C: 28)
- **Lines of code:** 147,169 (Go: 134,225, C: 12,944)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3,702 |
| `internal/` | 590 | 130,508 |
| `runtime/native/` (C code) | 28 | 12,944 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26,749 |
| 2 | `internal/vm` | 21,777 |
| 3 | `internal/backend/llvm` | 11,057 |
| 4 | `internal/mir` | 9,534 |
| 5 | `internal/parser` | 8,879 |
| 6 | `internal/hir` | 6,970 |
| 7 | `internal/driver` | 5,870 |
| 8 | `internal/lsp` | 4,926 |
| 9 | `internal/mono` | 4,630 |
| 10 | `internal/ast` | 4,422 |

## 🧪 Test files

- **Files:** 139
- **Lines of code:** 29,445

## 📈 Total volume (code + tests)

- **Files:** 780
- **Lines of code:** 176,614

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 75%, C: 7%)
- **Tests:** 16%

