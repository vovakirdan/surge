# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1041 (Go: 898, C: 143)
- **Lines of code:** 231742 (Go: 195491, C: 36251)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 865 | 189958 |
| `runtime/native/` (C code) | 143 | 36251 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45130 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21052 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 551
- **Lines of code:** 114461

## 📈 Total volume (code + tests)

- **Files:** 1592
- **Lines of code:** 346203

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 56%, C: 10%)
- **Tests:** 33%
