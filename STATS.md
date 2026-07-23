# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 810 (Go: 702, C: 108)
- **Lines of code:** 184456 (Go: 155316, C: 29140)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 672 | 149922 |
| `runtime/native/` (C code) | 108 | 29140 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32198 |
| 2 | `internal/vm` | 23524 |
| 3 | `internal/backend/llvm` | 15445 |
| 4 | `internal/mir` | 12099 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8534 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 281
- **Lines of code:** 64889

## 📈 Total volume (code + tests)

- **Files:** 1091
- **Lines of code:** 249345

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 62%, C: 11%)
- **Tests:** 26%

