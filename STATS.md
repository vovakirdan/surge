# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 682 (Go: 652, C: 30)
- **Lines of code:** 156352 (Go: 142236, C: 14116)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 623 | 137120 |
| `runtime/native/` (C code) | 30 | 14116 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28571 |
| 2 | `internal/vm` | 23197 |
| 3 | `internal/backend/llvm` | 12022 |
| 4 | `internal/mir` | 10281 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7162 |
| 7 | `internal/driver` | 6039 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 175
- **Lines of code:** 35889

## 📈 Total volume (code + tests)

- **Files:** 857
- **Lines of code:** 192241

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

