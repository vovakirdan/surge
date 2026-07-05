# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 722 (Go: 654, C: 68)
- **Lines of code:** 164620 (Go: 143607, C: 21013)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 68 | 21013 |

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

- **Files:** 213
- **Lines of code:** 46862

## 📈 Total volume (code + tests)

- **Files:** 935
- **Lines of code:** 211482

## 📊 Percentage breakdown

- **Main code (Go + C):** 77% (Go: 67%, C: 9%)
- **Tests:** 22%

