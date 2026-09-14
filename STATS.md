# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1195 (Go: 1014, C: 181)
- **Lines of code:** 259281 (Go: 215555, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 982 | 210582 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 55066 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19261 |
| 5 | `internal/parser` | 9553 |
| 6 | `internal/hir` | 9504 |
| 7 | `internal/driver` | 8078 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5697 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 862
- **Lines of code:** 172696

## 📈 Total volume (code + tests)

- **Files:** 2057
- **Lines of code:** 431977

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 49%, C: 10%)
- **Tests:** 39%
