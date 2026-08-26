# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1053 (Go: 910, C: 143)
- **Lines of code:** 233765 (Go: 197509, C: 36256)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 877 | 191976 |
| `runtime/native/` (C code) | 143 | 36256 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 46212 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21052 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 566
- **Lines of code:** 116892

## 📈 Total volume (code + tests)

- **Files:** 1619
- **Lines of code:** 350657

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 56%, C: 10%)
- **Tests:** 33%
