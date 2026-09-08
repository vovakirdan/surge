# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1143 (Go: 965, C: 178)
- **Lines of code:** 251206 (Go: 208104, C: 43102)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 933 | 203131 |
| `runtime/native/` (C code) | 178 | 43102 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49815 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22601 |
| 4 | `internal/mir` | 18864 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 723
- **Lines of code:** 149650

## 📈 Total volume (code + tests)

- **Files:** 1866
- **Lines of code:** 400856

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 51%, C: 10%)
- **Tests:** 37%
