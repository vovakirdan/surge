# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 819 (Go: 711, C: 108)
- **Lines of code:** 189151 (Go: 159915, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 681 | 154521 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34528 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 15995 |
| 4 | `internal/mir` | 12913 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8854 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 309
- **Lines of code:** 69976

## 📈 Total volume (code + tests)

- **Files:** 1128
- **Lines of code:** 259127

## 📊 Percentage breakdown

- **Main code (Go + C):** 72% (Go: 61%, C: 11%)
- **Tests:** 27%

