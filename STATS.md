# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1069 (Go: 920, C: 149)
- **Lines of code:** 238141 (Go: 199540, C: 38601)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 887 | 194007 |
| `runtime/native/` (C code) | 149 | 38601 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 47165 |
| 2 | `internal/vm` | 28602 |
| 3 | `internal/backend/llvm` | 21399 |
| 4 | `internal/mir` | 17367 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9487 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 615
- **Lines of code:** 128061

## 📈 Total volume (code + tests)

- **Files:** 1684
- **Lines of code:** 366202

## 📊 Percentage breakdown

- **Main code (Go + C):** 65% (Go: 54%, C: 10%)
- **Tests:** 34%
