# binary callable needs an exact origin contract

| Measure | Value |
|---|---:|
| Programs carrying it | 11 |
| Programs where it is the only root reason | 0 |
| Rows | 144 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_expr.go:274`.** A binary operator whose `MagicBinarySymbols` entry selects a method (`__add`, `__mul`, `__eq`, ...) with no exact origin contract. The missing fact is the operator method's body summary applied to its two operands.

## Corpus examples

1. **`sema/valid/fixed_array_view_operator_stays_in_frame.sg:23`**: `return a + b;`, and the same at lines 28, 34 and 40, on user operators over array views. The program also carries a projected-payload row, which N-FIELD-BORROW clears. Under the prototype this reason is its only root (measured).
2. **The copies:** `core_stdlib/array.sg:162`, `if self[i] == *value {`.

## Sound transfer and size

**N-OPERATOR-BODY, M, estimate 80 to 150 lines (read from code; not prototyped).** Route a binary operator with a selected method that has a body through the same path as the call `m(a, b)`. That means the callee's body summary, the G6 loan-discard guards, and implicit borrows passed as storage. N-CONCAT-USE (measured, 35 lines) is the certified special case for core `string +`.

## Unsoundness risk

- **The operators in the example return a value that borrows both operands.** The file's name says it "stays in frame". The transfer must carry both operands' storage to the result.
- **A missing operand would let a view leave the frame.** A pinned twin should return the result and expect SEM3139, the way `f14` (a slice of a window returned) already gets SEM3139 today (measured).

## DEBT-365 and DEBT-368

None named. P-VIEW2, a slice of a window returned, is the nearest listed form. It is already refused by SEM3139.

## Owner decision

None needed. For the copies, see `core-copies.md`.
