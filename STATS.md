# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1167 (Go: 986, C: 181)
- **Lines of code:** 254059 (Go: 210324, C: 43735)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 954 | 205351 |
| `runtime/native/` (C code) | 181 | 43735 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51217 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19159 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9502 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6431 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 810
- **Lines of code:** 164341

## 📈 Total volume (code + tests)

- **Files:** 1977
- **Lines of code:** 418400

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
