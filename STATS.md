# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1072 (Go: 919, C: 153)
- **Lines of code:** 237972 (Go: 199075, C: 38897)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 887 | 194102 |
| `runtime/native/` (C code) | 153 | 38897 |

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

- **Files:** 624
- **Lines of code:** 130072

## 📈 Total volume (code + tests)

- **Files:** 1696
- **Lines of code:** 368044

## 📊 Percentage breakdown

- **Main code (Go + C):** 64% (Go: 54%, C: 10%)
- **Tests:** 35%
