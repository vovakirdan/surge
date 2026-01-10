# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 598 (Go: 574, C: 24)
- **Lines of code:** 134729 (Go: 123733, C: 10996)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 20 | 3587 |
| `internal/` | 553 | 120131 |
| `runtime/native/` (C code) | 24 | 10996 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 25916 |
| 2 | `internal/vm` | 20844 |
| 3 | `internal/backend/llvm` | 10030 |
| 4 | `internal/mir` | 9009 |
| 5 | `internal/parser` | 8808 |
| 6 | `internal/hir` | 6736 |
| 7 | `internal/driver` | 5355 |
| 8 | `internal/mono` | 4566 |
| 9 | `internal/ast` | 4395 |
| 10 | `internal/diagfmt` | 4387 |

## 🧪 Test files

- **Files:** 117
- **Lines of code:** 26963

## 📈 Total volume (code + tests)

- **Files:** 715
- **Lines of code:** 161692

## 📊 Percentage breakdown

- **Main code (Go + C):** 83% (Go: 76%, C: 6%)
- **Tests:** 16%

