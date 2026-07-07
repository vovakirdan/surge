# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 734 (Go: 656, C: 78)
- **Lines of code:** 166201 (Go: 143854, C: 22347)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 626 | 138460 |
| `runtime/native/` (C code) | 78 | 22347 |

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

- **Files:** 218
- **Lines of code:** 48840

## 📈 Total volume (code + tests)

- **Files:** 952
- **Lines of code:** 215041

## 📊 Percentage breakdown

- **Main code (Go + C):** 77% (Go: 66%, C: 10%)
- **Tests:** 22%

