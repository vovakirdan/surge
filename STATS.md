# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 867 (Go: 756, C: 111)
- **Lines of code:** 197942 (Go: 168188, C: 29754)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 725 | 163249 |
| `runtime/native/` (C code) | 111 | 29754 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 35400 |
| 2 | `internal/vm` | 23813 |
| 3 | `internal/backend/llvm` | 16167 |
| 4 | `internal/mir` | 15789 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6436 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 390
- **Lines of code:** 85801

## 📈 Total volume (code + tests)

- **Files:** 1257
- **Lines of code:** 283743

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 59%, C: 10%)
- **Tests:** 30%

