# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 943 (Go: 821, C: 122)
- **Lines of code:** 211116 (Go: 179866, C: 31250)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 30 | 4931 |
| `internal/` | 790 | 174920 |
| `runtime/native/` (C code) | 122 | 31250 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 42976 |
| 2 | `internal/vm` | 23868 |
| 3 | `internal/backend/llvm` | 16265 |
| 4 | `internal/mir` | 16123 |
| 5 | `internal/parser` | 9357 |
| 6 | `internal/hir` | 9271 |
| 7 | `internal/driver` | 7630 |
| 8 | `internal/mono` | 6404 |
| 9 | `internal/lsp` | 5156 |
| 10 | `cmd/surge` | 4847 |

## 🧪 Test files

- **Files:** 455
- **Lines of code:** 97366

## 📈 Total volume (code + tests)

- **Files:** 1398
- **Lines of code:** 308482

## 📊 Percentage breakdown

- **Main code (Go + C):** 68% (Go: 58%, C: 10%)
- **Tests:** 31%
