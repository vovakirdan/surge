# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1151 (Go: 971, C: 180)
- **Lines of code:** 253220 (Go: 209564, C: 43656)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 939 | 204591 |
| `runtime/native/` (C code) | 180 | 43656 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22788 |
| 4 | `internal/mir` | 18899 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 743
- **Lines of code:** 154063

## 📈 Total volume (code + tests)

- **Files:** 1894
- **Lines of code:** 407283

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 51%, C: 10%)
- **Tests:** 37%
