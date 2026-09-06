# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1137 (Go: 960, C: 177)
- **Lines of code:** 249154 (Go: 206197, C: 42957)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 928 | 201224 |
| `runtime/native/` (C code) | 177 | 42957 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49282 |
| 2 | `internal/vm` | 30033 |
| 3 | `internal/backend/llvm` | 22419 |
| 4 | `internal/mir` | 18019 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 703
- **Lines of code:** 143059

## 📈 Total volume (code + tests)

- **Files:** 1840
- **Lines of code:** 392213

## 📊 Percentage breakdown

- **Main code (Go + C):** 63% (Go: 52%, C: 10%)
- **Tests:** 36%
