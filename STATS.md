# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 903 (Go: 781, C: 122)
- **Lines of code:** 206193 (Go: 174943, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 750 | 170004 |
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
| 8 | `internal/mono` | 6484 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4840 |

## 🧪 Test files

- **Files:** 418
- **Lines of code:** 91241

## 📈 Total volume (code + tests)

- **Files:** 1321
- **Lines of code:** 297434

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 58%, C: 10%)
- **Tests:** 30%

