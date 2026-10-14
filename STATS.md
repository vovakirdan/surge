# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1343 (Go: 1158, C: 185)
- **Lines of code:** 282482 (Go: 239310, C: 43172)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 1126 | 234337 |
| `runtime/native/` (C code) | 185 | 43172 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 76454 |
| 2 | `internal/vm` | 30426 |
| 3 | `internal/backend/llvm` | 23141 |
| 4 | `internal/mir` | 20125 |
| 5 | `internal/parser` | 9700 |
| 6 | `internal/hir` | 9637 |
| 7 | `internal/driver` | 8385 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5697 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 1159
- **Lines of code:** 221950

## 📈 Total volume (code + tests)

- **Files:** 2502
- **Lines of code:** 504432

## 📊 Percentage breakdown

- **Main code (Go + C):** 56% (Go: 47%, C: 8%)
- **Tests:** 43%
