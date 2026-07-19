# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 808 (Go: 700, C: 108)
- **Lines of code:** 183127 (Go: 154449, C: 28678)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 670 | 149055 |
| `runtime/native/` (C code) | 108 | 28678 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 32046 |
| 2 | `internal/vm` | 23523 |
| 3 | `internal/backend/llvm` | 15050 |
| 4 | `internal/mir` | 12055 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 8305 |
| 7 | `internal/driver` | 6381 |
| 8 | `internal/mono` | 5223 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 272
- **Lines of code:** 61604

## 📈 Total volume (code + tests)

- **Files:** 1080
- **Lines of code:** 244731

## 📊 Percentage breakdown

- **Main code (Go + C):** 74% (Go: 63%, C: 11%)
- **Tests:** 25%

