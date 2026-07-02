# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 691 (Go: 654, C: 37)
- **Lines of code:** 159473 (Go: 143607, C: 15866)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 624 | 138213 |
| `runtime/native/` (C code) | 37 | 15866 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28574 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12534 |
| 4 | `internal/mir` | 10464 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7156 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 192
- **Lines of code:** 39957

## 📈 Total volume (code + tests)

- **Files:** 883
- **Lines of code:** 199430

## 📊 Percentage breakdown

- **Main code (Go + C):** 79% (Go: 72%, C: 7%)
- **Tests:** 20%

