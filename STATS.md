# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1021 (Go: 890, C: 131)
- **Lines of code:** 227104 (Go: 194521, C: 32583)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 858 | 189016 |
| `runtime/native/` (C code) | 131 | 32583 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45041 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20718 |
| 4 | `internal/mir` | 17072 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 523
- **Lines of code:** 110418

## 📈 Total volume (code + tests)

- **Files:** 1544
- **Lines of code:** 337522

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
