# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1148 (Go: 968, C: 180)
- **Lines of code:** 252687 (Go: 209208, C: 43479)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 936 | 204235 |
| `runtime/native/` (C code) | 180 | 43479 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 50881 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22601 |
| 4 | `internal/mir` | 18899 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 731
- **Lines of code:** 152174

## 📈 Total volume (code + tests)

- **Files:** 1879
- **Lines of code:** 404861

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 51%, C: 10%)
- **Tests:** 37%
