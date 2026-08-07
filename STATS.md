# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 956 (Go: 834, C: 122)
- **Lines of code:** 213188 (Go: 181938, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 803 | 176993 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 17702 |
| 4 | `internal/mir` | 16657 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 464
- **Lines of code:** 99428

## 📈 Total volume (code + tests)

- **Files:** 1420
- **Lines of code:** 312616

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 9%)
- **Tests:** 31%
