# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 672 (Go: 643, C: 29)
- **Lines of code:** 152607 (Go: 139454, C: 13153)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4635 |
| `internal/` | 615 | 134804 |
| `runtime/native/` (C code) | 29 | 13153 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28111 |
| 2 | `internal/vm` | 22382 |
| 3 | `internal/backend/llvm` | 11557 |
| 4 | `internal/mir` | 10023 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7070 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4635 |

## 🧪 Test files

- **Files:** 156
- **Lines of code:** 32965

## 📈 Total volume (code + tests)

- **Files:** 828
- **Lines of code:** 185572

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 75%, C: 7%)
- **Tests:** 17%

