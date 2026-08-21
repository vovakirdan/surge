# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1025 (Go: 893, C: 132)
- **Lines of code:** 227466 (Go: 194748, C: 32718)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 860 | 189215 |
| `runtime/native/` (C code) | 132 | 32718 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45062 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 20805 |
| 4 | `internal/mir` | 17078 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 528
- **Lines of code:** 111168

## 📈 Total volume (code + tests)

- **Files:** 1553
- **Lines of code:** 338634

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
