# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 685 (Go: 654, C: 31)
- **Lines of code:** 158008 (Go: 143361, C: 14647)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 137979 |
| `runtime/native/` (C code) | 31 | 14647 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28574 |
| 2 | `internal/vm` | 23337 |
| 3 | `internal/backend/llvm` | 12456 |
| 4 | `internal/mir` | 10464 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7156 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 181
- **Lines of code:** 36891

## 📈 Total volume (code + tests)

- **Files:** 866
- **Lines of code:** 194899

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

