# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 921 (Go: 799, C: 122)
- **Lines of code:** 207792 (Go: 176542, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 768 | 171603 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 41105 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16265 |
| 4 | `internal/mir` | 15878 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9222 |
| 7 | `internal/driver` | 7165 |
| 8 | `internal/mono` | 6508 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 431
- **Lines of code:** 93063

## 📈 Total volume (code + tests)

- **Files:** 1352
- **Lines of code:** 300855

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 58%, C: 10%)
- **Tests:** 30%
