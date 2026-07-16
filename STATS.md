# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 805 (Go: 697, C: 108)
- **Lines of code:** 181929 (Go: 153350, C: 28579)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 667 | 147956 |
| `runtime/native/` (C code) | 108 | 28579 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 31704 |
| 2 | `internal/vm` | 23506 |
| 3 | `internal/backend/llvm` | 14864 |
| 4 | `internal/mir` | 11962 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7848 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 268
- **Lines of code:** 59915

## 📈 Total volume (code + tests)

- **Files:** 1073
- **Lines of code:** 241844

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

