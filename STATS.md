# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1143 (Go: 965, C: 178)
- **Lines of code:** 250688 (Go: 207586, C: 43102)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 933 | 202613 |
| `runtime/native/` (C code) | 178 | 43102 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 49704 |
| 2 | `internal/vm` | 30031 |
| 3 | `internal/backend/llvm` | 22557 |
| 4 | `internal/mir` | 18793 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 715
- **Lines of code:** 147685

## 📈 Total volume (code + tests)

- **Files:** 1858
- **Lines of code:** 398373

## 📊 Percentage breakdown

- **Main code (Go + C):** 62% (Go: 52%, C: 10%)
- **Tests:** 37%
