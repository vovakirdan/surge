# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1141 (Go: 963, C: 178)
- **Lines of code:** 250179 (Go: 207170, C: 43009)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 931 | 202197 |
| `runtime/native/` (C code) | 178 | 43009 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49564 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22506 |
| 4 | `internal/mir` | 18618 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 708
- **Lines of code:** 145131

## 📈 Total volume (code + tests)

- **Files:** 1849
- **Lines of code:** 395310

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
