# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1061 (Go: 916, C: 145)
- **Lines of code:** 235328 (Go: 198640, C: 36688)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 883 | 193107 |
| `runtime/native/` (C code) | 145 | 36688 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47058 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17192 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9485 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 583
- **Lines of code:** 119692

## 📈 Total volume (code + tests)

- **Files:** 1644
- **Lines of code:** 355020

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 55%, C: 10%)
- **Tests:** 33%
