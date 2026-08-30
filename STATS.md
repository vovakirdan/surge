# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1071 (Go: 920, C: 151)
- **Lines of code:** 238382 (Go: 199635, C: 38747)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 887 | 194102 |
| `runtime/native/` (C code) | 151 | 38747 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47165 |
| 2 | `internal/vm` | 28602 |
| 3 | `internal/backend/llvm` | 21419 |
| 4 | `internal/mir` | 17426 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 617
- **Lines of code:** 128477

## 📈 Total volume (code + tests)

- **Files:** 1688
- **Lines of code:** 366859

## 📊 Percentage breakdown

- **Main code (Go + C):** 64% (Go: 54%, C: 10%)
- **Tests:** 35%
