# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 799 (Go: 693, C: 106)
- **Lines of code:** 179706 (Go: 151779, C: 27927)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 663 | 146385 |
| `runtime/native/` (C code) | 106 | 27927 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 31068 |
| 2 | `internal/vm` | 23499 |
| 3 | `internal/backend/llvm` | 14279 |
| 4 | `internal/mir` | 11944 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7719 |
| 7 | `internal/driver` | 6204 |
| 8 | `internal/mono` | 5210 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 262
- **Lines of code:** 59126

## 📈 Total volume (code + tests)

- **Files:** 1061
- **Lines of code:** 238832

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

