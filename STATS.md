# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 800 (Go: 694, C: 106)
- **Lines of code:** 180474 (Go: 152479, C: 27995)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 664 | 147085 |
| `runtime/native/` (C code) | 106 | 27995 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 31224 |
| 2 | `internal/vm` | 23499 |
| 3 | `internal/backend/llvm` | 14696 |
| 4 | `internal/mir` | 11962 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7815 |
| 7 | `internal/driver` | 6204 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 266
- **Lines of code:** 59621

## 📈 Total volume (code + tests)

- **Files:** 1066
- **Lines of code:** 240095

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

