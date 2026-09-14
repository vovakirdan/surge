# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1170 (Go: 989, C: 181)
- **Lines of code:** 254598 (Go: 210872, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 957 | 205899 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51217 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19227 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9504 |
| 7 | `internal/driver` | 7708 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 816
- **Lines of code:** 166040

## 📈 Total volume (code + tests)

- **Files:** 1986
- **Lines of code:** 420638

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
