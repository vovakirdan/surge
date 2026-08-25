# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1037 (Go: 896, C: 141)
- **Lines of code:** 230704 (Go: 195065, C: 35639)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 863 | 189532 |
| `runtime/native/` (C code) | 141 | 35639 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 21073 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 537
- **Lines of code:** 112567

## 📈 Total volume (code + tests)

- **Files:** 1574
- **Lines of code:** 343271

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 56%, C: 10%)
- **Tests:** 32%
