# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 685 (Go: 654, C: 31)
- **Lines of code:** 157190 (Go: 142874, C: 14316)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 137496 |
| `runtime/native/` (C code) | 31 | 14316 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28571 |
| 2 | `internal/vm` | 23263 |
| 3 | `internal/backend/llvm` | 12162 |
| 4 | `internal/mir` | 10330 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7156 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 177
- **Lines of code:** 36569

## 📈 Total volume (code + tests)

- **Files:** 862
- **Lines of code:** 193759

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 73%, C: 7%)
- **Tests:** 18%

