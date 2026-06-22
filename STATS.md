# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 683 (Go: 653, C: 30)
- **Lines of code:** 156817 (Go: 142585, C: 14232)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 623 | 137319 |
| `runtime/native/` (C code) | 30 | 14232 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28571 |
| 2 | `internal/vm` | 23197 |
| 3 | `internal/backend/llvm` | 12160 |
| 4 | `internal/mir` | 10319 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7162 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 176
- **Lines of code:** 36515

## 📈 Total volume (code + tests)

- **Files:** 859
- **Lines of code:** 193332

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

