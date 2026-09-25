# expression kind 12 needs an origin transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 4 |
| Programs where it is the only root reason | 3 |
| Rows | 9 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

Kind 12 is `ast.ExprTupleIndex`.

## Where it is raised

- **`internal/sema/return_origin_expr.go:158`.** The expression catch-all, which has no case for a tuple element read. The missing fact is the origin of `t.N`.

## Corpus examples

1. **`sema/valid/tuple_access.sg:4`**: `return pair.0;`, plus `:9` `return own t.1;` and `:14` `return t.0.1;`. This is its only root reason.
2. **`vm_async_suite/t15_fairness_round_robin.sg:36`**: `if pair.0 {`, plus `:37` `pair.1`. This is its only root reason.

The others are `hir/tuples.sg:6` and `vm_tuples/tuple_literals.sg:25-32`.

## Sound transfer and size

- **N-TUPLE, S, prototype 80 lines together with destructuring, measured.**
  - A tuple element is a sub-place of its tuple. Its storage is the tuple's storage, or the referent's when the tuple is reached through a reference.
  - Its value is fresh when the element type is reference-free and not a loan carrier. Otherwise it keeps all of the tuple's roots, an over-approximation that never drops one.
- **What it frees.** `hir/tuples.sg`, `sema/valid/tuple_access.sg` and `vm_async_suite/t15_fairness_round_robin.sg`. With the destructuring half it also frees `vm_tuples/tuple_literals.sg`.

## Unsoundness risk

- **Low.** Typing already refuses a reference inside a tuple: canaries `f01` and `f02` get SEM3138 on the base and under the prototype (measured).
- **`t15` becomes a runtime result.** DEBT.md:229 (ST-RUNOUT) calls `t15_fairness_round_robin` "still unfinished and not a runtime result". Under the prototype it builds and runs: its VM output is byte-equal to `.out`, and on LLVM it is equal as a multiset of lines in 3 of 3 runs, as its `.order-backends` sidecar requires (measured). The runner-matrix watch should add it when N-TUPLE lands.

## DEBT-365 and DEBT-368

ST-RUNOUT note above.

## Owner decision

None needed.
