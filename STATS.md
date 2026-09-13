# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1166 (Go: 985, C: 181)
- **Lines of code:** 254011 (Go: 210276, C: 43735)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 953 | 205303 |
| `runtime/native/` (C code) | 181 | 43735 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51217 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19111 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9502 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6431 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 808
- **Lines of code:** 163897

## 📈 Total volume (code + tests)

- **Files:** 1974
- **Lines of code:** 417908

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
