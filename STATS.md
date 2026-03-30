# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 664 (Go: 636, C: 28)
- **Lines of code:** 151244 (Go: 138316, C: 12928)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 26 | 4600 |
| `internal/` | 609 | 133701 |
| `runtime/native/` (C code) | 28 | 12928 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 27787 |
| 2 | `internal/vm` | 21891 |
| 3 | `internal/backend/llvm` | 11369 |
| 4 | `internal/mir` | 9989 |
| 5 | `internal/parser` | 8932 |
| 6 | `internal/hir` | 7053 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4600 |

## 🧪 Test files

- **Files:** 149
- **Lines of code:** 31901

## 📈 Total volume (code + tests)

- **Files:** 813
- **Lines of code:** 183145

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

