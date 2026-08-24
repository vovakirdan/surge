# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1032 (Go: 895, C: 137)
- **Lines of code:** 229607 (Go: 195001, C: 34606)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 862 | 189468 |
| `runtime/native/` (C code) | 137 | 34606 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 21009 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 532
- **Lines of code:** 111863

## 📈 Total volume (code + tests)

- **Files:** 1564
- **Lines of code:** 341470

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 10%)
- **Tests:** 32%
