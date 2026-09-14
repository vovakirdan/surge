# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1173 (Go: 992, C: 181)
- **Lines of code:** 255071 (Go: 211345, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 960 | 206372 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51579 |
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

- **Files:** 822
- **Lines of code:** 166844

## 📈 Total volume (code + tests)

- **Files:** 1995
- **Lines of code:** 421915

## 📊 Percentage breakdown

- **Main code (Go + C):** 60% (Go: 50%, C: 10%)
- **Tests:** 39%
