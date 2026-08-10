# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 980 (Go: 858, C: 122)
- **Lines of code:** 218757 (Go: 187339, C: 31418)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 826 | 181834 |
| `runtime/native/` (C code) | 122 | 31418 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43694 |
| 2 | `internal/vm` | 27428 |
| 3 | `internal/backend/llvm` | 18254 |
| 4 | `internal/mir` | 16755 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9313 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 481
- **Lines of code:** 103055

## 📈 Total volume (code + tests)

- **Files:** 1461
- **Lines of code:** 321812

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
