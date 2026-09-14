# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1208 (Go: 1027, C: 181)
- **Lines of code:** 261419 (Go: 217693, C: 43726)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 995 | 212720 |
| `runtime/native/` (C code) | 181 | 43726 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 57204 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19261 |
| 5 | `internal/parser` | 9553 |
| 6 | `internal/hir` | 9504 |
| 7 | `internal/driver` | 8078 |
| 8 | `internal/mono` | 6911 |
| 9 | `internal/lsp` | 5697 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 888
- **Lines of code:** 178973

## 📈 Total volume (code + tests)

- **Files:** 2096
- **Lines of code:** 440392

## 📊 Percentage breakdown

- **Main code (Go + C):** 59% (Go: 49%, C: 9%)
- **Tests:** 40%
