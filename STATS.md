# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 951 (Go: 829, C: 122)
- **Lines of code:** 212028 (Go: 180778, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 798 | 175833 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16699 |
| 4 | `internal/mir` | 16585 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 465
- **Lines of code:** 99202

## 📈 Total volume (code + tests)

- **Files:** 1416
- **Lines of code:** 311230

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 10%)
- **Tests:** 31%
