# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1060 (Go: 915, C: 145)
- **Lines of code:** 235107 (Go: 198419, C: 36688)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 882 | 192886 |
| `runtime/native/` (C code) | 145 | 36688 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 46839 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17190 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9485 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 583
- **Lines of code:** 119427

## 📈 Total volume (code + tests)

- **Files:** 1643
- **Lines of code:** 354534

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 55%, C: 10%)
- **Tests:** 33%
