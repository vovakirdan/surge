# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1035 (Go: 894, C: 141)
- **Lines of code:** 230814 (Go: 195249, C: 35565)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 861 | 189716 |
| `runtime/native/` (C code) | 141 | 35565 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 21257 |
| 4 | `internal/mir` | 17115 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 538
- **Lines of code:** 112567

## 📈 Total volume (code + tests)

- **Files:** 1573
- **Lines of code:** 343381

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 56%, C: 10%)
- **Tests:** 32%
