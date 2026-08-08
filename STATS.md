# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 976 (Go: 854, C: 122)
- **Lines of code:** 217464 (Go: 186104, C: 31360)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 822 | 180599 |
| `runtime/native/` (C code) | 122 | 31360 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 26974 |
| 3 | `internal/backend/llvm` | 18144 |
| 4 | `internal/mir` | 16715 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 476
- **Lines of code:** 102942

## 📈 Total volume (code + tests)

- **Files:** 1452
- **Lines of code:** 320406

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
