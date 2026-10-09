# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1328 (Go: 1144, C: 184)
- **Lines of code:** 281162 (Go: 236669, C: 44493)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 1112 | 231696 |
| `runtime/native/` (C code) | 184 | 44493 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 74401 |
| 2 | `internal/vm` | 30467 |
| 3 | `internal/backend/llvm` | 23117 |
| 4 | `internal/mir` | 19673 |
| 5 | `internal/parser` | 9700 |
| 6 | `internal/hir` | 9587 |
| 7 | `internal/driver` | 8275 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5697 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 1141
- **Lines of code:** 223347

## 📈 Total volume (code + tests)

- **Files:** 2469
- **Lines of code:** 504509

## 📊 Percentage breakdown

- **Main code (Go + C):** 55% (Go: 46%, C: 8%)
- **Tests:** 44%
