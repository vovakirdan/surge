# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 873 (Go: 756, C: 117)
- **Lines of code:** 198588 (Go: 168233, C: 30355)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 725 | 163294 |
| `runtime/native/` (C code) | 117 | 30355 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 35400 |
| 2 | `internal/vm` | 23813 |
| 3 | `internal/backend/llvm` | 16171 |
| 4 | `internal/mir` | 15789 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6436 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 394
- **Lines of code:** 86443

## 📈 Total volume (code + tests)

- **Files:** 1267
- **Lines of code:** 285031

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 59%, C: 10%)
- **Tests:** 30%

