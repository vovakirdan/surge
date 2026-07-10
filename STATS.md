# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 777 (Go: 679, C: 98)
- **Lines of code:** 173749 (Go: 148401, C: 25348)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 649 | 143007 |
| `runtime/native/` (C code) | 98 | 25348 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30062 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 13180 |
| 4 | `internal/mir` | 11401 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7460 |
| 7 | `internal/driver` | 6194 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 245
- **Lines of code:** 55811

## 📈 Total volume (code + tests)

- **Files:** 1022
- **Lines of code:** 229560

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 64%, C: 11%)
- **Tests:** 24%

