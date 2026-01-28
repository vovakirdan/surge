# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 650 (Go: 622, C: 28)
- **Lines of code:** 148796 (Go: 135852, C: 12944)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 24 | 4291 |
| `internal/` | 597 | 131546 |
| `runtime/native/` (C code) | 28 | 12944 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 26781 |
| 2 | `internal/vm` | 21845 |
| 3 | `internal/backend/llvm` | 11122 |
| 4 | `internal/mir` | 9822 |
| 5 | `internal/parser` | 8879 |
| 6 | `internal/hir` | 6988 |
| 7 | `internal/driver` | 6055 |
| 8 | `internal/lsp` | 5082 |
| 9 | `internal/mono` | 4630 |
| 10 | `internal/ast` | 4422 |

## 🧪 Test files

- **Files:** 141
- **Lines of code:** 29890

## 📈 Total volume (code + tests)

- **Files:** 791
- **Lines of code:** 178686

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 7%)
- **Tests:** 16%

