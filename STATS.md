# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 677 (Go: 647, C: 30)
- **Lines of code:** 154201 (Go: 141002, C: 13199)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 619 | 136318 |
| `runtime/native/` (C code) | 30 | 13199 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28288 |
| 2 | `internal/vm` | 22982 |
| 3 | `internal/backend/llvm` | 11786 |
| 4 | `internal/mir` | 10281 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7094 |
| 7 | `internal/driver` | 6039 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 170
- **Lines of code:** 34572

## 📈 Total volume (code + tests)

- **Files:** 847
- **Lines of code:** 188773

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 74%, C: 6%)
- **Tests:** 18%

