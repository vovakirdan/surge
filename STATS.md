# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1157 (Go: 976, C: 181)
- **Lines of code:** 253484 (Go: 209758, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 944 | 204785 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22878 |
| 4 | `internal/mir` | 18939 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 777
- **Lines of code:** 158654

## 📈 Total volume (code + tests)

- **Files:** 1934
- **Lines of code:** 412138

## 📊 Percentage breakdown

- **Main code (Go + C):** 61% (Go: 50%, C: 10%)
- **Tests:** 38%
