# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 750 (Go: 668, C: 82)
- **Lines of code:** 169777 (Go: 146532, C: 23245)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 28 | 4819 |
| `internal/` | 638 | 141138 |
| `runtime/native/` (C code) | 82 | 23245 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 30009 |
| 2 | `internal/vm` | 23493 |
| 3 | `internal/backend/llvm` | 12673 |
| 4 | `internal/mir` | 10633 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 7177 |
| 7 | `internal/driver` | 6160 |
| 8 | `internal/lsp` | 5156 |
| 9 | `internal/mono` | 5122 |
| 10 | `cmd/surge` | 4819 |

## 🧪 Test files

- **Files:** 236
- **Lines of code:** 53181

## 📈 Total volume (code + tests)

- **Files:** 986
- **Lines of code:** 222958

## 📊 Percentage breakdown

- **Main code (Go + C):** 76% (Go: 65%, C: 10%)
- **Tests:** 23%

