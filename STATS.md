# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1025 (Go: 893, C: 132)
- **Lines of code:** 227585 (Go: 194772, C: 32813)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 860 | 189239 |
| `runtime/native/` (C code) | 132 | 32813 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 20817 |
| 4 | `internal/mir` | 17078 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 528
- **Lines of code:** 111228

## 📈 Total volume (code + tests)

- **Files:** 1553
- **Lines of code:** 338813

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
