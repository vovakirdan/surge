# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 814 (Go: 706, C: 108)
- **Lines of code:** 185299 (Go: 156063, C: 29236)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 676 | 150669 |
| `runtime/native/` (C code) | 108 | 29236 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32460 |
| 2 | `internal/vm` | 23530 |
| 3 | `internal/backend/llvm` | 15619 |
| 4 | `internal/mir` | 12305 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8534 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5275 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 285
- **Lines of code:** 65282

## 📈 Total volume (code + tests)

- **Files:** 1099
- **Lines of code:** 250581

## 📊 Percentage breakdown

- **Main code (Go + C):** 73% (Go: 62%, C: 11%)
- **Tests:** 26%

