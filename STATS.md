# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1153 (Go: 973, C: 180)
- **Lines of code:** 253282 (Go: 209630, C: 43652)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 941 | 204657 |
| `runtime/native/` (C code) | 180 | 43652 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22742 |
| 4 | `internal/mir` | 18945 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 750
- **Lines of code:** 154963

## 📈 Total volume (code + tests)

- **Files:** 1903
- **Lines of code:** 408245

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 51%, C: 10%)
- **Tests:** 37%
