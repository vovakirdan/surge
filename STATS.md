# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 905 (Go: 783, C: 122)
- **Lines of code:** 206206 (Go: 174956, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 752 | 170017 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 39958 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16265 |
| 4 | `internal/mir` | 15842 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9222 |
| 7 | `internal/driver` | 6807 |
| 8 | `internal/mono` | 6497 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 421
- **Lines of code:** 91277

## 📈 Total volume (code + tests)

- **Files:** 1326
- **Lines of code:** 297483

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 58%, C: 10%)
- **Tests:** 30%

