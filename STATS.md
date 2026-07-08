# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 745 (Go: 667, C: 78)
- **Lines of code:** 168328 (Go: 145981, C: 22347)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 637 | 140587 |
| `runtime/native/` (C code) | 78 | 22347 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 29937 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12534 |
| 4 | `internal/mir` | 10466 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7177 |
| 7 | `internal/driver` | 6062 |
| 8 | `internal/lsp` | 5156 |
| 9 | `internal/mono` | 5122 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 231
- **Lines of code:** 51350

## 📈 Total volume (code + tests)

- **Files:** 976
- **Lines of code:** 219678

## 📊 Percentage breakdown

- **Main code (Go + C):** 76% (Go: 66%, C: 10%)
- **Tests:** 23%

