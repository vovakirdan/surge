# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 685 (Go: 654, C: 31)
- **Lines of code:** 158311 (Go: 143595, C: 14716)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 31 | 14716 |

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

- **Files:** 181
- **Lines of code:** 36942

## 📈 Total volume (code + tests)

- **Files:** 866
- **Lines of code:** 195253

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

