# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 784 (Go: 682, C: 102)
- **Lines of code:** 175429 (Go: 149034, C: 26395)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 652 | 143640 |
| `runtime/native/` (C code) | 102 | 26395 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30193 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 13555 |
| 4 | `internal/mir` | 11505 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7462 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 250
- **Lines of code:** 56544

## 📈 Total volume (code + tests)

- **Files:** 1034
- **Lines of code:** 231973

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 64%, C: 11%)
- **Tests:** 24%

