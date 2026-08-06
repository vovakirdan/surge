# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 947 (Go: 825, C: 122)
- **Lines of code:** 211706 (Go: 180456, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 794 | 175511 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/mir` | 16585 |
| 4 | `internal/backend/llvm` | 16377 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 462
- **Lines of code:** 98617

## 📈 Total volume (code + tests)

- **Files:** 1409
- **Lines of code:** 310323

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 10%)
- **Tests:** 31%
