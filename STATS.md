# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1134 (Go: 957, C: 177)
- **Lines of code:** 247920 (Go: 205183, C: 42737)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 925 | 200210 |
| `runtime/native/` (C code) | 177 | 42737 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49188 |
| 2 | `internal/vm` | 30033 |
| 3 | `internal/backend/llvm` | 21535 |
| 4 | `internal/mir` | 18005 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 700
- **Lines of code:** 142058

## 📈 Total volume (code + tests)

- **Files:** 1834
- **Lines of code:** 389978

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
