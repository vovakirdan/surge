# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 873 (Go: 756, C: 117)
- **Lines of code:** 198551 (Go: 168211, C: 30340)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4924 |
| `internal/` | 725 | 163272 |
| `runtime/native/` (C code) | 117 | 30340 |

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

- **Files:** 393
- **Lines of code:** 86421

## 📈 Total volume (code + tests)

- **Files:** 1266
- **Lines of code:** 284972

## 📊 Percentage breakdown

- **Main code (Go + C):** 69% (Go: 59%, C: 10%)
- **Tests:** 30%

