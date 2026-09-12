# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1155 (Go: 975, C: 180)
- **Lines of code:** 253385 (Go: 209733, C: 43652)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 943 | 204760 |
| `runtime/native/` (C code) | 180 | 43652 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22849 |
| 4 | `internal/mir` | 18943 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 764
- **Lines of code:** 156788

## 📈 Total volume (code + tests)

- **Files:** 1919
- **Lines of code:** 410173

## 📊 Percentage breakdown

- **Main code (Go + C):** 61% (Go: 51%, C: 10%)
- **Tests:** 38%
