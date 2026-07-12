# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 788 (Go: 685, C: 103)
- **Lines of code:** 176682 (Go: 149879, C: 26803)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 655 | 144485 |
| `runtime/native/` (C code) | 103 | 26803 |

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

- **Files:** 252
- **Lines of code:** 57209

## 📈 Total volume (code + tests)

- **Files:** 1040
- **Lines of code:** 233891

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 64%, C: 11%)
- **Tests:** 24%

