# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1024 (Go: 892, C: 132)
- **Lines of code:** 227199 (Go: 194592, C: 32607)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 859 | 189059 |
| `runtime/native/` (C code) | 132 | 32607 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45041 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20761 |
| 4 | `internal/mir` | 17072 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 524
- **Lines of code:** 110597

## 📈 Total volume (code + tests)

- **Files:** 1548
- **Lines of code:** 337796

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
