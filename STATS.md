# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 898 (Go: 776, C: 122)
- **Lines of code:** 206139 (Go: 174889, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 745 | 169950 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 39958 |
| 2 | `internal/vm` | 23860 |
| 3 | `internal/backend/llvm` | 16231 |
| 4 | `internal/mir` | 15842 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9210 |
| 7 | `internal/driver` | 6807 |
| 8 | `internal/mono` | 6484 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 412
- **Lines of code:** 91150

## 📈 Total volume (code + tests)

- **Files:** 1310
- **Lines of code:** 297289

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 58%, C: 10%)
- **Tests:** 30%
