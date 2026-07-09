# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 745 (Go: 667, C: 78)
- **Lines of code:** 168415 (Go: 146068, C: 22347)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 637 | 140674 |
| `runtime/native/` (C code) | 78 | 22347 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 29964 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12534 |
| 4 | `internal/mir` | 10466 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7177 |
| 7 | `internal/driver` | 6105 |
| 8 | `internal/lsp` | 5156 |
| 9 | `internal/mono` | 5122 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 232
- **Lines of code:** 51927

## 📈 Total volume (code + tests)

- **Files:** 977
- **Lines of code:** 220342

## 📊 Percentage breakdown

- **Main code (Go + C):** 76% (Go: 66%, C: 10%)
- **Tests:** 23%

