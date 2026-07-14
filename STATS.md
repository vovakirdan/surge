# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 799 (Go: 693, C: 106)
- **Lines of code:** 180039 (Go: 152098, C: 27941)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 663 | 146704 |
| `runtime/native/` (C code) | 106 | 27941 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 31224 |
| 2 | `internal/vm` | 23499 |
| 3 | `internal/backend/llvm` | 14315 |
| 4 | `internal/mir` | 11962 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7815 |
| 7 | `internal/driver` | 6204 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 265
- **Lines of code:** 59457

## 📈 Total volume (code + tests)

- **Files:** 1064
- **Lines of code:** 239496

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

