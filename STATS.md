# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 951 (Go: 829, C: 122)
- **Lines of code:** 211980 (Go: 180730, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4930 |
| `internal/` | 798 | 175785 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16651 |
| 4 | `internal/mir` | 16585 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6421 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4846 |

## 🧪 Test files

- **Files:** 464
- **Lines of code:** 98965

## 📈 Total volume (code + tests)

- **Files:** 1415
- **Lines of code:** 310945

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 10%)
- **Tests:** 31%
