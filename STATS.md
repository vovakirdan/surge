# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 682 (Go: 652, C: 30)
- **Lines of code:** 156502 (Go: 142279, C: 14223)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 623 | 137163 |
| `runtime/native/` (C code) | 30 | 14223 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28571 |
| 2 | `internal/vm` | 23197 |
| 3 | `internal/backend/llvm` | 12027 |
| 4 | `internal/mir` | 10319 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7162 |
| 7 | `internal/driver` | 6039 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 175
- **Lines of code:** 36227

## 📈 Total volume (code + tests)

- **Files:** 857
- **Lines of code:** 192729

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

