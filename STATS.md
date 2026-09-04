# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1136 (Go: 959, C: 177)
- **Lines of code:** 248339 (Go: 205602, C: 42737)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 927 | 200629 |
| `runtime/native/` (C code) | 177 | 42737 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49188 |
| 2 | `internal/vm` | 30033 |
| 3 | `internal/backend/llvm` | 21937 |
| 4 | `internal/mir` | 18013 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 701
- **Lines of code:** 142344

## 📈 Total volume (code + tests)

- **Files:** 1837
- **Lines of code:** 390683

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
