# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 787 (Go: 684, C: 103)
- **Lines of code:** 176458 (Go: 149680, C: 26778)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 654 | 144286 |
| `runtime/native/` (C code) | 103 | 26778 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30363 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 13798 |
| 4 | `internal/mir` | 11583 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7462 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 251
- **Lines of code:** 57044

## 📈 Total volume (code + tests)

- **Files:** 1038
- **Lines of code:** 233502

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 64%, C: 11%)
- **Tests:** 24%

