# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1140 (Go: 962, C: 178)
- **Lines of code:** 249713 (Go: 206708, C: 43005)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 930 | 201735 |
| `runtime/native/` (C code) | 178 | 43005 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49282 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22488 |
| 4 | `internal/mir` | 18463 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 705
- **Lines of code:** 144043

## 📈 Total volume (code + tests)

- **Files:** 1845
- **Lines of code:** 393756

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
