# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 720 (Go: 654, C: 66)
- **Lines of code:** 163893 (Go: 143607, C: 20286)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 66 | 20286 |

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

- **Files:** 204
- **Lines of code:** 44220

## 📈 Total volume (code + tests)

- **Files:** 924
- **Lines of code:** 208113

## 📊 Percentage breakdown

- **Main code (Go + C):** 78% (Go: 69%, C: 9%)
- **Tests:** 21%

