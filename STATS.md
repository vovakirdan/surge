# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1005 (Go: 879, C: 126)
- **Lines of code:** 224981 (Go: 192770, C: 32211)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 847 | 187265 |
| `runtime/native/` (C code) | 126 | 32211 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 44861 |
| 2 | `internal/vm` | 28261 |
| 3 | `internal/backend/llvm` | 19616 |
| 4 | `internal/mir` | 16852 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 507
- **Lines of code:** 108090

## 📈 Total volume (code + tests)

- **Files:** 1512
- **Lines of code:** 333071

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
