# Codebase stats for the Surge compiler

---

## 📊 Main code (without tests)

- **Files:** 1164 (Go: 983, C: 181)
- **Lines of code:** 253964 (Go: 210229, C: 43735)

## 📁 Directory breakdown

| Directory | Files | Lines |
|------------|--------|-------|
| `cmd/` | 31 | 4958 |
| `internal/` | 951 | 205256 |
| `runtime/native/` (C code) | 181 | 43735 |

## 🏆 Top 10 packages by size

| # | Package | Lines |
|---|-------|-------|
| 1 | `internal/sema` | 51200 |
| 2 | `internal/vm` | 30012 |
| 3 | `internal/backend/llvm` | 22884 |
| 4 | `internal/mir` | 19081 |
| 5 | `internal/parser` | 9544 |
| 6 | `internal/hir` | 9502 |
| 7 | `internal/driver` | 7710 |
| 8 | `internal/mono` | 6431 |
| 9 | `internal/lsp` | 5695 |
| 10 | `cmd/surge` | 4874 |

## 🧪 Test files

- **Files:** 797
- **Lines of code:** 161806

## 📈 Total volume (code + tests)

- **Files:** 1961
- **Lines of code:** 415770

## 📊 Percentage breakdown

- **Main code (Go + C):** 61% (Go: 50%, C: 10%)
- **Tests:** 38%
