# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 817 (Go: 709, C: 108)
- **Lines of code:** 186776 (Go: 157540, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 679 | 152146 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32954 |
| 2 | `internal/vm` | 23658 |
| 3 | `internal/backend/llvm` | 15940 |
| 4 | `internal/mir` | 12629 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8627 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 297
- **Lines of code:** 67510

## 📈 Total volume (code + tests)

- **Files:** 1114
- **Lines of code:** 254286

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 61%, C: 11%)
- **Tests:** 26%

