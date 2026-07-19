# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 810 (Go: 702, C: 108)
- **Lines of code:** 183453 (Go: 154749, C: 28704)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 672 | 149355 |
| `runtime/native/` (C code) | 108 | 28704 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32140 |
| 2 | `internal/vm` | 23524 |
| 3 | `internal/backend/llvm` | 15054 |
| 4 | `internal/mir` | 12062 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8497 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 275
- **Lines of code:** 63789

## 📈 Total volume (code + tests)

- **Files:** 1085
- **Lines of code:** 247242

## 📊 Percentage breakdown

- **Main code (Go + C):** 74% (Go: 62%, C: 11%)
- **Tests:** 25%

