# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1031 (Go: 894, C: 137)
- **Lines of code:** 228624 (Go: 194847, C: 33777)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 861 | 189314 |
| `runtime/native/` (C code) | 137 | 33777 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 45074 |
| 2 | `internal/vm` | 28215 |
| 3 | `internal/backend/llvm` | 20892 |
| 4 | `internal/mir` | 17078 |
| 5 | `internal/hir` | 9487 |
| 6 | `internal/parser` | 9435 |
| 7 | `internal/driver` | 7660 |
| 8 | `internal/mono` | 6422 |
| 9 | `internal/lsp` | 5182 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 530
- **Lines of code:** 111480

## 📈 Total volume (code + tests)

- **Files:** 1561
- **Lines of code:** 340104

## 📊 Percentage breakdown

- **Main code (Go + C):** 67% (Go: 57%, C: 9%)
- **Tests:** 32%
