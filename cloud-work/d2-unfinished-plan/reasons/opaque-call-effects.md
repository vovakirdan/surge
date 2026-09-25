# opaque call may change reference-bearing or callable contents

| Measure | Value |
|---|---:|
| Programs carrying it | 11 |
| Programs where it is the only root reason | 1 |
| Rows | 261 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_calls.go:244`.** An opaque callee (body-less, or generic with no instance) whose effects on its reference arguments cannot be proven, or which has uncertified loan sinks. The analysis taints the external cells.

## Corpus examples

1. **`sema/valid/clone_semantics/task_clone_uninstantiated_generic.sg:9`**: `return handle.clone();`, with `handle: &Task<T>` inside `fn duplicate<T>`, which nothing instantiates. This is 1 row, its only root.
2. **The copies:** `core_stdlib/array.sg:6`, `rt_array_reserve(a, new_cap);`.

## Sound transfer and size

For the non-core program the fact needed is about a Task handle. The return-origin half is small: a certificate saying that the core Task clone leaves its receiver unchanged and returns another handle to the same task. The other half is what that second handle holds, which is the task check's, and `Task` is not yet a counted handle (`internal/types/refcounted_handle.go:21-22`). This is owner question 2.

## Unsoundness risk

A Task clone certificate that ignores what the task borrows would open R-i's neighbourhood: a task handed out past the frame it borrows from.

## DEBT-365 and DEBT-368

R-i (DEBT.md:229) and Wave D4b.

## Owner decision

Yes. See `owner-q2-task-values.md`. For the copies, see `core-copies.md`.
