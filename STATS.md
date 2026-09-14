# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1204 (Go: 1023, C: 181)
- **Lines of code:** 260603 (Go: 216877, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 991 | 211904 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 56388 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19261 |
| 5 | `internal/parser` | 9553 |
| 6 | `internal/hir` | 9504 |
| 7 | `internal/driver` | 8078 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5697 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 872
- **Lines of code:** 175147

## 📈 Total volume (code + tests)

- **Files:** 2076
- **Lines of code:** 435750

## 📊 Percentage breakdown

- **Main code (Go + C):** 59% (Go: 49%, C: 10%)
- **Tests:** 40%
