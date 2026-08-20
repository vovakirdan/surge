# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1009 (Go: 883, C: 126)
- **Lines of code:** 226302 (Go: 193870, C: 32432)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 851 | 188365 |
| `runtime/native/` (C code) | 126 | 32432 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45032 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20121 |
| 4 | `internal/mir` | 17027 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 514
- **Lines of code:** 109835

## 📈 Total volume (code + tests)

- **Files:** 1523
- **Lines of code:** 336137

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
