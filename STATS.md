# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1006 (Go: 880, C: 126)
- **Lines of code:** 225401 (Go: 193016, C: 32385)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 848 | 187511 |
| `runtime/native/` (C code) | 126 | 32385 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44861 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 19641 |
| 4 | `internal/mir` | 16852 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 508
- **Lines of code:** 108475

## 📈 Total volume (code + tests)

- **Files:** 1514
- **Lines of code:** 333876

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
