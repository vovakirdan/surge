# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 790 (Go: 686, C: 104)
- **Lines of code:** 177669 (Go: 150126, C: 27543)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 656 | 144732 |
| `runtime/native/` (C code) | 104 | 27543 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30412 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 13976 |
| 4 | `internal/mir` | 11590 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7462 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 254
- **Lines of code:** 57629

## 📈 Total volume (code + tests)

- **Files:** 1044
- **Lines of code:** 235298

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

