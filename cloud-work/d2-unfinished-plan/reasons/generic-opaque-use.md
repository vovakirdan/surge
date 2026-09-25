# generic opaque use requires its type-dependent effect transfer

| Measure | Value |
|---|---:|
| Programs carrying it | 6 |
| Programs where it is the only root reason | 0 |
| Rows | 6 |
| Of those programs, `core_stdlib` copies | 1 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_declarations.go:252`** (`checkGenericPromise`). A finalized use of a body-less generic declaration whose type-dependent effects or requirements are not proven for that instance.

## Corpus examples

All five non-core carriers are the same site: `core/option.sg:12`, `_ => default::<T>();`, with `T` a core runtime handle.

1. **`sema/valid/concurrency/task_container_suspend_safe.sg:13`**: `let t = q.pop().safe();`
2. **`vm_async_suite/t14_loop_join.sg:15`**: `let t = ts.pop().safe();`

The sixth carrier is the `core_stdlib/string.sg` copy.

## Sound transfer and size

The fact is the same one `opaque-result-classification.md` needs: a core runtime handle's default is its null sentinel. N-DEFHANDLE answers both rows in all five programs (S, 6 lines, measured).

## Unsoundness risk

See `opaque-result-classification.md`. Measured there: the VM cannot build a default runtime handle and panics with VM1999 when one is reached. This fails closed. N-DEFHANDLE must not answer NoBorrowedState for a Task, which is R-i's fence.

## DEBT-365 and DEBT-368

R-i: see above.

## Owner decision

None needed. For the copy, see `core-copies.md`.
