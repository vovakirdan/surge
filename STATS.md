# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 754 (Go: 670, C: 84)
- **Lines of code:** 171040 (Go: 147288, C: 23752)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 640 | 141894 |
| `runtime/native/` (C code) | 84 | 23752 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30009 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12673 |
| 4 | `internal/mir` | 10974 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7460 |
| 7 | `internal/driver` | 6160 |
| 8 | `internal/mono` | 5264 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 238
- **Lines of code:** 54483

## 📈 Total volume (code + tests)

- **Files:** 992
- **Lines of code:** 225523

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 65%, C: 10%)
- **Tests:** 24%

