# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 723 (Go: 654, C: 69)
- **Lines of code:** 164701 (Go: 143607, C: 21094)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 69 | 21094 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28574 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12534 |
| 4 | `internal/mir` | 10464 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7156 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 214
- **Lines of code:** 47482

## 📈 Total volume (code + tests)

- **Files:** 937
- **Lines of code:** 212183

## 📊 Percentage breakdown

- **Main code (Go + C):** 77% (Go: 67%, C: 9%)
- **Tests:** 22%

