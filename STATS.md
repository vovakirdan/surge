# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 830 (Go: 722, C: 108)
- **Lines of code:** 192463 (Go: 163185, C: 29278)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4822 |
| `internal/` | 692 | 157788 |
| `runtime/native/` (C code) | 108 | 29278 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 34744 |
| 2 | `internal/vm` | 23749 |
| 3 | `internal/backend/llvm` | 16065 |
| 4 | `internal/mir` | 15065 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9012 |
| 7 | `internal/driver` | 6411 |
| 8 | `internal/mono` | 5344 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4822 |

## 🧪 Test files

- **Files:** 349
- **Lines of code:** 78789

## 📈 Total volume (code + tests)

- **Files:** 1179
- **Lines of code:** 271252

## 📊 Percentage breakdown

- **Main code (Go + C):** 70% (Go: 60%, C: 10%)
- **Tests:** 29%

