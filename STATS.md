# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 641 (Go: 613, C: 28)
- **Lines of code:** 147473 (Go: 134529, C: 12944)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 22 | 3702 |
| `internal/` | 590 | 130812 |
| `runtime/native/` (C code) | 28 | 12944 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26773 |
| 2 | `internal/vm` | 21787 |
| 3 | `internal/backend/llvm` | 11130 |
| 4 | `internal/mir` | 9534 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 5904 |
| 8 | `internal/lsp` | 5071 |
| 9 | `internal/mono` | 4630 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 139
- **Lines of code:** 29764

## 📈 Total volume (code + tests)

- **Files:** 780
- **Lines of code:** 177237

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 75%, C: 7%)
- **Tests:** 16%

