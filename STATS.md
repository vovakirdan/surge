# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1062 (Go: 917, C: 145)
- **Lines of code:** 235710 (Go: 198744, C: 36966)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 884 | 193211 |
| `runtime/native/` (C code) | 145 | 36966 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47080 |
| 2 | `internal/vm` | 28584 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17192 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9485 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 589
- **Lines of code:** 120677

## 📈 Total volume (code + tests)

- **Files:** 1651
- **Lines of code:** 356387

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 55%, C: 10%)
- **Tests:** 33%
