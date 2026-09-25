# destructuring needs projected origin facts

| Measure | Value |
|---|---:|
| Programs carrying it | 3 |
| Programs where it is the only root reason | 2 |
| Rows | 5 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_stmt.go:56`.** Every `let` with a pattern. The missing fact is which part of the subject each pattern name receives.

## Corpus examples

1. **`sema/valid/tuple_destructure_call.sg:7`**: `let (a, b, c) = produce();`. This is 1 row, its only root.
2. **`sema/valid/tuple_destructure.sg:4`**: `let (x, y) = pair;`, plus `:8` `let (a, b) = (10, 20);`. This is its only root.

The third is `vm_tuples/tuple_literals.sg:8` and `:14`.

## Sound transfer and size

- **The destructuring half of N-TUPLE, measured.** A tuple pattern of identifiers is accepted, and nested tuple patterns too. Each name must resolve to its own `let` binding with a recorded binding type. Each name takes the subject's value: fresh when the binding's type is reference-free and not a loan carrier, and otherwise all of the subject's roots. Any other pattern keeps the row.
- **What it frees.** `tuple_destructure.sg` and `tuple_destructure_call.sg`. With the tuple-index half it also frees `tuple_literals.sg`. `tuple_literals.sg` runs to its golden `.out` on both backends.

## Unsoundness risk

Low. A tuple holding a reference is refused by typing: canary `f02` gets SEM3015 and SEM3138 on the base and under the prototype (measured).

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
