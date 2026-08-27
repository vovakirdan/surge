# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1060 (Go: 915, C: 145)
- **Lines of code:** 234893 (Go: 198102, C: 36791)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 883 | 193129 |
| `runtime/native/` (C code) | 145 | 36791 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47080 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17192 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9485 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 584
- **Lines of code:** 119975

## 📈 Total volume (code + tests)

- **Files:** 1644
- **Lines of code:** 354868

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 55%, C: 10%)
- **Tests:** 33%
