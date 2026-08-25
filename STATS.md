# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1033 (Go: 894, C: 139)
- **Lines of code:** 230683 (Go: 195232, C: 35451)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 861 | 189699 |
| `runtime/native/` (C code) | 139 | 35451 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 21240 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 534
- **Lines of code:** 112204

## 📈 Total volume (code + tests)

- **Files:** 1567
- **Lines of code:** 342887

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 56%, C: 10%)
- **Tests:** 32%
