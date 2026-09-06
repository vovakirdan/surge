# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1140 (Go: 962, C: 178)
- **Lines of code:** 249875 (Go: 206866, C: 43009)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 930 | 201893 |
| `runtime/native/` (C code) | 178 | 43009 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49380 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22490 |
| 4 | `internal/mir` | 18521 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 706
- **Lines of code:** 144659

## 📈 Total volume (code + tests)

- **Files:** 1846
- **Lines of code:** 394534

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
