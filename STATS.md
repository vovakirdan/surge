# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 756 (Go: 672, C: 84)
- **Lines of code:** 171656 (Go: 147852, C: 23804)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 642 | 142458 |
| `runtime/native/` (C code) | 84 | 23804 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30009 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12894 |
| 4 | `internal/mir` | 11260 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7460 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 239
- **Lines of code:** 54927

## 📈 Total volume (code + tests)

- **Files:** 995
- **Lines of code:** 226583

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 65%, C: 10%)
- **Tests:** 24%

