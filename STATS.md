# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 684 (Go: 654, C: 30)
- **Lines of code:** 156927 (Go: 142695, C: 14232)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 137429 |
| `runtime/native/` (C code) | 30 | 14232 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28571 |
| 2 | `internal/vm` | 23197 |
| 3 | `internal/backend/llvm` | 12161 |
| 4 | `internal/mir` | 10330 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7156 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 177
- **Lines of code:** 36568

## 📈 Total volume (code + tests)

- **Files:** 861
- **Lines of code:** 193495

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

