# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 676 (Go: 646, C: 30)
- **Lines of code:** 153992 (Go: 140793, C: 13199)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 618 | 136109 |
| `runtime/native/` (C code) | 30 | 13199 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28288 |
| 2 | `internal/vm` | 22982 |
| 3 | `internal/backend/llvm` | 11783 |
| 4 | `internal/mir` | 10178 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7094 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 167
- **Lines of code:** 34165

## 📈 Total volume (code + tests)

- **Files:** 843
- **Lines of code:** 188157

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 74%, C: 7%)
- **Tests:** 18%

