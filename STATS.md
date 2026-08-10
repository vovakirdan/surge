# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 980 (Go: 858, C: 122)
- **Lines of code:** 218806 (Go: 187377, C: 31429)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 826 | 181872 |
| `runtime/native/` (C code) | 122 | 31429 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 43732 |
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

- **Files:** 482
- **Lines of code:** 103362

## 📈 Total volume (code + tests)

- **Files:** 1462
- **Lines of code:** 322168

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 58%, C: 9%)
- **Tests:** 32%
