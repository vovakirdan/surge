# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 812 (Go: 704, C: 108)
- **Lines of code:** 184777 (Go: 155554, C: 29223)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 674 | 150160 |
| `runtime/native/` (C code) | 108 | 29223 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32324 |
| 2 | `internal/vm` | 23524 |
| 3 | `internal/backend/llvm` | 15532 |
| 4 | `internal/mir` | 12115 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8534 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 283
- **Lines of code:** 64998

## 📈 Total volume (code + tests)

- **Files:** 1095
- **Lines of code:** 249775

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 62%, C: 11%)
- **Tests:** 26%

