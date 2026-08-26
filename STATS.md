# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1059 (Go: 914, C: 145)
- **Lines of code:** 234976 (Go: 198288, C: 36688)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 881 | 192755 |
| `runtime/native/` (C code) | 145 | 36688 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 46817 |
| 2 | `internal/vm` | 28572 |
| 3 | `internal/backend/llvm` | 21086 |
| 4 | `internal/mir` | 17190 |
| 5 | `internal/hir` | 9485 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 581
- **Lines of code:** 119268

## 📈 Total volume (code + tests)

- **Files:** 1640
- **Lines of code:** 354244

## 📊 Percentage breakdown

- **Main code (Go + C):** 66% (Go: 55%, C: 10%)
- **Tests:** 33%
