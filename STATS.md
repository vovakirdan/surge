# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1028 (Go: 893, C: 135)
- **Lines of code:** 228056 (Go: 194772, C: 33284)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 860 | 189239 |
| `runtime/native/` (C code) | 135 | 33284 |

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
- **Lines of code:** 111231

## 📈 Total volume (code + tests)

- **Files:** 1556
- **Lines of code:** 339287

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
