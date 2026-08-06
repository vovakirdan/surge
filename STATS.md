# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 938 (Go: 816, C: 122)
- **Lines of code:** 210226 (Go: 178976, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 785 | 174037 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42496 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16265 |
| 4 | `internal/mir` | 15832 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7425 |
| 8 | `internal/mono` | 6505 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 450
- **Lines of code:** 96267

## 📈 Total volume (code + tests)

- **Files:** 1388
- **Lines of code:** 306493

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 10%)
- **Tests:** 31%
