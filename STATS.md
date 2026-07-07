# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 732 (Go: 654, C: 78)
- **Lines of code:** 165919 (Go: 143607, C: 22312)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 78 | 22312 |

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

- **Files:** 216
- **Lines of code:** 48492

## 📈 Total volume (code + tests)

- **Files:** 948
- **Lines of code:** 214411

## 📊 Percentage breakdown

- **Main code (Go + C):** 77% (Go: 66%, C: 10%)
- **Tests:** 22%

