# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 826 (Go: 718, C: 108)
- **Lines of code:** 190831 (Go: 161595, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 688 | 156201 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34744 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 14219 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 322
- **Lines of code:** 73266

## 📈 Total volume (code + tests)

- **Files:** 1148
- **Lines of code:** 264097

## 📊 Percentage breakdown

- **Main code (Go + C):** 72% (Go: 61%, C: 11%)
- **Tests:** 27%

