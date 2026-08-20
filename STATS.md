# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1008 (Go: 882, C: 126)
- **Lines of code:** 226216 (Go: 193767, C: 32449)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 850 | 188262 |
| `runtime/native/` (C code) | 126 | 32449 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45032 |
| 2 | `internal/vm` | 28221 |
| 3 | `internal/backend/llvm` | 20193 |
| 4 | `internal/mir` | 16852 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9357 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 514
- **Lines of code:** 110157

## 📈 Total volume (code + tests)

- **Files:** 1522
- **Lines of code:** 336373

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
