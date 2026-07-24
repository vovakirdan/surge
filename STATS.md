# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 811 (Go: 703, C: 108)
- **Lines of code:** 184601 (Go: 155449, C: 29152)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 673 | 150055 |
| `runtime/native/` (C code) | 108 | 29152 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32244 |
| 2 | `internal/vm` | 23524 |
| 3 | `internal/backend/llvm` | 15523 |
| 4 | `internal/mir` | 12099 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8534 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 282
- **Lines of code:** 64880

## 📈 Total volume (code + tests)

- **Files:** 1093
- **Lines of code:** 249481

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 62%, C: 11%)
- **Tests:** 26%

