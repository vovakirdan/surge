# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1156 (Go: 976, C: 180)
- **Lines of code:** 253645 (Go: 209993, C: 43652)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 944 | 205020 |
| `runtime/native/` (C code) | 180 | 43652 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51172 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22848 |
| 4 | `internal/mir` | 18945 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9475 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 762
- **Lines of code:** 156793

## 📈 Total volume (code + tests)

- **Files:** 1918
- **Lines of code:** 410438

## 📊 Percentage breakdown

- **Main code (Go + C):** 61% (Go: 51%, C: 10%)
- **Tests:** 38%
