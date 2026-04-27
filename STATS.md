# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 680 (Go: 650, C: 30)
- **Lines of code:** 154705 (Go: 141506, C: 13199)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 27 | 4669 |
| `internal/` | 622 | 136822 |
| `runtime/native/` (C code) | 30 | 13199 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 28562 |
| 2 | `internal/vm` | 22982 |
| 3 | `internal/backend/llvm` | 11948 |
| 4 | `internal/mir` | 10281 |
| 5 | `internal/parser` | 8960 |
| 6 | `internal/hir` | 7162 |
| 7 | `internal/driver` | 6039 |
| 8 | `internal/lsp` | 5152 |
| 9 | `internal/mono` | 5120 |
| 10 | `cmd/surge` | 4669 |

## 🧪 Test files

- **Files:** 172
- **Lines of code:** 34971

## 📈 Total volume (code + tests)

- **Files:** 852
- **Lines of code:** 189676

## 📊 Percentage breakdown

- **Main code (Go + C):** 81% (Go: 74%, C: 6%)
- **Tests:** 18%
