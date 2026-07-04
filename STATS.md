# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1374 (Go: 1307, C: 67)
- **Lines of code:** 307214 (Go: 286654, C: 20560)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 67 | 20560 |

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

- **Files:** 212
- **Lines of code:** 46495

## 📈 Total volume (code + tests)

- **Files:** 1586
- **Lines of code:** 353709

## 📊 Percentage breakdown

- **Main code (Go + C):** 86% (Go: 81%, C: 5%)
- **Tests:** 13%

