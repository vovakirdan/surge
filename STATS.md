# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 674 (Go: 645, C: 29)
- **Lines of code:** 153359 (Go: 140200, C: 13159)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 617 | 135516 |
| `runtime/native/` (C code) | 29 | 13159 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28232 |
| 2 | `internal/vm` | 22822 |
| 3 | `internal/backend/llvm` | 11554 |
| 4 | `internal/mir` | 10030 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7094 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 164
- **Lines of code:** 33801

## 📈 Total volume (code + tests)

- **Files:** 838
- **Lines of code:** 187160

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 74%, C: 7%)
- **Tests:** 18%

