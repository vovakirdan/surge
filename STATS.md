# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1153 (Go: 972, C: 181)
- **Lines of code:** 253313 (Go: 209589, C: 43724)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 940 | 204616 |
| `runtime/native/` (C code) | 181 | 43724 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22817 |
| 4 | `internal/mir` | 18895 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 751
- **Lines of code:** 155538

## 📈 Total volume (code + tests)

- **Files:** 1904
- **Lines of code:** 408851

## 📊 Percentage breakdown

- **Main code (Go + C):** 61% (Go: 51%, C: 10%)
- **Tests:** 38%
