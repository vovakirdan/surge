# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1143 (Go: 965, C: 178)
- **Lines of code:** 250464 (Go: 207453, C: 43011)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 933 | 202480 |
| `runtime/native/` (C code) | 178 | 43011 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49679 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22506 |
| 4 | `internal/mir` | 18777 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 710
- **Lines of code:** 145751

## 📈 Total volume (code + tests)

- **Files:** 1853
- **Lines of code:** 396215

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
