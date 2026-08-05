# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 916 (Go: 794, C: 122)
- **Lines of code:** 207016 (Go: 175766, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 763 | 170827 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 40389 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16265 |
| 4 | `internal/mir` | 15878 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9222 |
| 7 | `internal/driver` | 7142 |
| 8 | `internal/mono` | 6497 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 428
- **Lines of code:** 92223

## 📈 Total volume (code + tests)

- **Files:** 1344
- **Lines of code:** 299239

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 58%, C: 10%)
- **Tests:** 30%

