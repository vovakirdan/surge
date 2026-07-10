# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 779 (Go: 680, C: 99)
- **Lines of code:** 174352 (Go: 148644, C: 25708)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 650 | 143250 |
| `runtime/native/` (C code) | 99 | 25708 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30064 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 13348 |
| 4 | `internal/mir` | 11471 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7460 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 246
- **Lines of code:** 56027

## 📈 Total volume (code + tests)

- **Files:** 1025
- **Lines of code:** 230379

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 64%, C: 11%)
- **Tests:** 24%

