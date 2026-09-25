# expression kind 27 needs an origin transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 24 |
| Programs where it is the only root reason | 24 |
| Rows | 24 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

Kind 27 is `ast.ExprOn` (`internal/ast/expr_types.go`, the `ExprKind` enumeration). The row stands on `spawn on ...`.

## Where it is raised

- **`internal/sema/return_origin_expr.go:145-150`.** The `ExprOn` case sends a plain `on` to `onCrossing`. A `spawn on` falls through to the catch-all `expression kind %d needs an origin transfer`. The missing fact is what a far task started by `spawn on` captures and holds past this frame.

## Corpus examples

1. **`crossing/block03/valid/spawn_on_positive_pool.sg:7`**: `let t = spawn on pool {`. This is 1 row, its only root.
2. **`crossing/integration/valid/_integration_spawn_on_then_await.sg:3`**: `let task: far Task<int> = spawn on dst {`. This is 1 row, its only root, and the file is outside harness scope.

## Transfer, size and risk

Not this plan's. The brief assigns this row to the in-flight packets N-TASK-27S and TC-XB. Every program that carries it (24) has it as its only root reason, so all 24 become ok once every kind-27 row goes, provided nothing else changes. That is a projection over `census.json`, not a run.

## DEBT-365 and DEBT-368

DEBT.md:229: "N-TASK kind 27 names no residual (R-h was measured refused) and needs only the first line green".

## Owner decision

None needed here.
