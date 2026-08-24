# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1032 (Go: 895, C: 137)
- **Lines of code:** 229744 (Go: 195014, C: 34730)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 862 | 189481 |
| `runtime/native/` (C code) | 137 | 34730 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 21022 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 532
- **Lines of code:** 111939

## 📈 Total volume (code + tests)

- **Files:** 1564
- **Lines of code:** 341683

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 10%)
- **Tests:** 32%
