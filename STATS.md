# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1156 (Go: 976, C: 180)
- **Lines of code:** 253392 (Go: 209742, C: 43650)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 944 | 204769 |
| `runtime/native/` (C code) | 180 | 43650 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50905 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22794 |
| 4 | `internal/mir` | 19035 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9502 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6431 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 746
- **Lines of code:** 154694

## 📈 Total volume (code + tests)

- **Files:** 1902
- **Lines of code:** 408086

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 51%, C: 10%)
- **Tests:** 37%
