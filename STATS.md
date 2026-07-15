# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 802 (Go: 694, C: 108)
- **Lines of code:** 181289 (Go: 152736, C: 28553)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 664 | 147342 |
| `runtime/native/` (C code) | 108 | 28553 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 31275 |
| 2 | `internal/vm` | 23506 |
| 3 | `internal/backend/llvm` | 14862 |
| 4 | `internal/mir` | 11962 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7848 |
| 7 | `internal/driver` | 6204 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 268
- **Lines of code:** 59899

## 📈 Total volume (code + tests)

- **Files:** 1070
- **Lines of code:** 241188

## 📊 Percentage breakdown

- **Main code (Go + C):** 75% (Go: 63%, C: 11%)
- **Tests:** 24%

