# opaque result borrowed-state classification is unsupported

| Measure | Value |
|---|---:|
| Programs carrying it | 24 |
| Programs where it is the only root reason | 6 |
| Rows | 509 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_conditions.go:80`** is the body walk. **`internal/sema/return_origin_condition_uses.go:54`** is the check of a finalized generic use.
- The requirement on an opaque callee's result (NoBorrowedState or Defaultable) meets a type whose borrowed state the analysis cannot classify (read from code: `returnOriginTypeView.requirement`, `internal/sema/return_origin_requirements.go`). The missing fact is different in each group of programs.

| Group | Programs | Missing fact | Answered by |
|---|---:|---|---|
| `default::<T>()` with `T` a core runtime handle, reached through `Option.safe()` | 5 | a handle's default holds nothing | N-DEFHANDLE (measured) |
| `stdlib/time` `Duration`, an `@copy @intrinsic` struct with one `int64` field | 5 | `Duration` is a plain word | N-STDLIB-TIME (measured) |
| `rt_entropy_bytes`, the result `Erring<byte[], Error>` | 3 | a fresh buffer | N-STDLIB-ENTROPY (measured) |
| a Task produced through an `async fn` value | 1 | what that Task borrows | owner question 2 |
| `core_stdlib` copies | 10 | identity | owner question 1 |

## Corpus examples

1. **`vm_async_suite/t14_loop_join.sg:15`**: `let t = ts.pop().safe();`. The row lands on `core/intrinsics.sg:777` (`pub fn default<T>() -> T;`), reached through `core/option.sg:12` (`_ => default::<T>();`).
2. **`sema/valid/directives/stdlib_time_import/main.sg:6`**: `return time.monotonic_now();`, with rows at `stdlib/time/time.sg:16` (`pub fn monotonic_now() -> Duration;`) and `:33` (`pub fn sub(...) -> Duration;`).

## Sound transfers and size

- **N-DEFHANDLE, S, prototype 6 lines.** In the Defaultable branch for a struct, a type for which `IsRuntimeHandleType` holds meets Defaultable with no requirement. LLVM defines that default as a null pointer (`internal/backend/llvm/emit_intrinsics_default.go:67-73`: "Runtime-owned handles use a null pointer as their uninitialized sentinel"). A null names no runtime object, so it holds nothing of any frame.
- **N-STDLIB-TIME, S.** The prototype allowed the attributes of a one-field `int64` struct in `stdlib/time/time.sg`, keyed by source key, in 3 lines. A real packet must be an identity certificate for the `Duration` declaration. A general "`@intrinsic` struct" rule would be unsound, because `Range`, `Task`, `Channel` and `BytesView` are `@intrinsic` too (`core/intrinsics.sg:151`, `191`, `248`, `729`) and hold runtime objects or loans. Estimate 30 to 50 lines (read from code).
- **N-STDLIB-ENTROPY, S, prototype 13 lines.** A row `rt_entropy_bytes` in `returnOriginFreshContainerRows`, with a length parameter and `uint8` elements, wrapped in `Erring`. The certificate is keyed to the body-less intrinsic declared in `stdlib/entropy/entropy.sg:7`. The runtime builds a fresh buffer from the length alone (`runtime/native/rt_entropy.c:180-212`, `internal/vm/runtime_entropy.go:37-49`; read from code).

Measured together with the rest of the prototype: 13 of the 14 non-core programs diagnose clean. The exception is `fn_type_async.sg`. `t14_loop_join.sg` is the only one of these programs with a golden `.out`, and it runs to it on both backends.

## Unsoundness risk

- **N-DEFHANDLE must answer only Defaultable, never NoBorrowedState.** NoBorrowedState on a joined Task payload is the fence of R-i. The prototype touched only the Defaultable branch. Row `borrowing_task_payload_stays_refused` of `TestAnalyzeTaskAwaits` passes under it (measured).
- **The VM cannot build a default runtime handle, which was found while measuring this packet.** With N-DEFHANDLE on, `let mut q: Task<int>[] = []; let t = q.pop().safe();` builds. It then panics on the VM with `panic VM1999: storage: type#1508 has 1 members but 0 layout offsets` at `core/option.sg:12:18`, while on LLVM it runs and exits 5. `let mut ch: Channel<int>;` panics the same way on the VM. The VM builds the default from the handle's layout (`internal/vm/intrinsic_default.go:145-162`) instead of a null sentinel (read from code). This fails closed, as a panic rather than a memory error. None of the five freed golden programs reaches the default at run time. It still has to be recorded, and ideally fixed before N-DEFHANDLE lands.
- **The time and entropy certificates are low risk** while they stay identity-keyed.

## DEBT-365 and DEBT-368

R-i: see above. The `fn_type_async.sg` group is owner question 2.

## Owner decision

Only for `fn_type_async.sg`: see `owner-q2-task-values.md`. For the copies, see `core-copies.md`.
