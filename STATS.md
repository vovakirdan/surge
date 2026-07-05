# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 721 (Go: 654, C: 67)
- **Lines of code:** 164332 (Go: 143607, C: 20725)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 67 | 20725 |

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
- **Lines of code:** 46675

## 📈 Total volume (code + tests)

- **Files:** 934
- **Lines of code:** 211007

## 📊 Percentage breakdown

- **Main code (Go + C):** 77% (Go: 68%, C: 9%)
- **Tests:** 22%

