# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1067 (Go: 918, C: 149)
- **Lines of code:** 236985 (Go: 199138, C: 37847)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 885 | 193605 |
| `runtime/native/` (C code) | 149 | 37847 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47138 |
| 2 | `internal/vm` | 28606 |
| 3 | `internal/backend/llvm` | 21158 |
| 4 | `internal/mir` | 17229 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 604
- **Lines of code:** 124793

## 📈 Total volume (code + tests)

- **Files:** 1671
- **Lines of code:** 361778

## 📊 Percentage breakdown

- **Main code (Go + C):** 65% (Go: 55%, C: 10%)
- **Tests:** 34%
