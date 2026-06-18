# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 682 (Go: 652, C: 30)
- **Lines of code:** 155959 (Go: 142226, C: 13733)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 623 | 137110 |
| `runtime/native/` (C code) | 30 | 13733 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28562 |
| 2 | `internal/vm` | 23197 |
| 3 | `internal/backend/llvm` | 12021 |
| 4 | `internal/mir` | 10281 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7162 |
| 7 | `internal/driver` | 6039 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 174
- **Lines of code:** 35795

## 📈 Total volume (code + tests)

- **Files:** 856
- **Lines of code:** 191754

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 74%, C: 7%)
- **Tests:** 18%
