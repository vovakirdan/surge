# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1055 (Go: 910, C: 145)
- **Lines of code:** 234216 (Go: 197573, C: 36643)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 877 | 192040 |
| `runtime/native/` (C code) | 145 | 36643 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 46212 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17145 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 570
- **Lines of code:** 117616

## 📈 Total volume (code + tests)

- **Files:** 1625
- **Lines of code:** 351832

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 56%, C: 10%)
- **Tests:** 33%
