# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 807 (Go: 699, C: 108)
- **Lines of code:** 182873 (Go: 154195, C: 28678)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 669 | 148801 |
| `runtime/native/` (C code) | 108 | 28678 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32046 |
| 2 | `internal/vm` | 23523 |
| 3 | `internal/backend/llvm` | 15050 |
| 4 | `internal/mir` | 12055 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8051 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 272
- **Lines of code:** 61492

## 📈 Total volume (code + tests)

- **Files:** 1079
- **Lines of code:** 244365

## 📊 Percentage breakdown

- **Main code (Go + C):** 74% (Go: 63%, C: 11%)
- **Tests:** 25%

