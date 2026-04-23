# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 673 (Go: 644, C: 29)
- **Lines of code:** 152992 (Go: 139833, C: 13159)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4635 |
| `internal/` | 616 | 135183 |
| `runtime/native/` (C code) | 29 | 13159 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28232 |
| 2 | `internal/vm` | 22496 |
| 3 | `internal/backend/llvm` | 11554 |
| 4 | `internal/mir` | 10023 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7094 |
| 7 | `internal/driver` | 6009 |
| 8 | `internal/mono` | 5120 |
| 9 | `internal/lsp` | 5082 |
| 10 | `cmd/surge` | 4635 |

## 🧪 Test files

- **Files:** 162
- **Lines of code:** 33561

## 📈 Total volume (code + tests)

- **Files:** 835
- **Lines of code:** 186553

## 📊 Percentage breakdown

- **Main code (Go + C):** 82% (Go: 74%, C: 7%)
- **Tests:** 17%

