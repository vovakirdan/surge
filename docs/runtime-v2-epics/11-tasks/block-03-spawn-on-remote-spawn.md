# Block 03: `spawn on dst { ... }` Remote Spawn Test Matrix

This document defines the documentary test-case matrix for Epic 11 Block 3:
`spawn on dst { ... }`, `far Task<T>`, and remote task lifecycle operations.
It is not an implementation plan and does not add parser, semantic-analysis,
lowering, runtime, or public-example work.

Block 3 depends on the Block 2 placement destination model and the Block 4
crossing-contract rules. It does not redefine `on dst { ... }` immediate
crossing semantics, `crosses`, `@shard_movable`, `@shard_pinned`, `@send`,
`@nosend`, or `@local spawn`.

## Implementation Status

**Implemented; `crossinggate` gate enabled (`Block3Enabled = true`).** Parser +
semantic analysis land the `spawn on dst { ... }` and `far Task<T>`
await/cancel surface, with backend-unavailable guards (`FUT7015`–`FUT7017`)
reported through `buildpipeline.Compile`. Golden fixtures under
`testdata/golden/crossing/block03/{valid,invalid}/` are activated (14 positives,
29 sema negatives with committed sidecars; the three `FUT7015`–`FUT7017`
backend-unavailable negatives stay `_`-prefixed with `// EXPECT-STAGE: backend`
and are exercised only through `internal/crossinggate`).

Deviations from the original matrix, all from the mid-block design change that
removes the explicit `crosses` keyword (crossing effect now inferred into
metadata, not required at a site):

- **`crosses` retired.** The keyword is no longer required for `spawn on`,
  `far Task<T>.await()`, or `.cancel()`; it is stripped from every fixture
  source. The `crosses` propagation matrix (X03/X04) and lifecycle rows
  `T07`/`T08` are **RETIRED**: `SEM3162`/`SEM3163`/`SEM3164` are no longer
  emitted for spawn-on/await/cancel. `SEM3162` remains live only via Block 2's
  `on`-crossing emission until the separate post-Block-3 grammar-cleanup commit.
- **Four negatives deleted** (their asserted requirement no longer exists):
  `_spawn_on_negative_without_crosses` (X03), `_spawn_on_negative_crosses_call_propagation`
  (X04), `_spawn_on_negative_await_without_crosses` (T07),
  `_spawn_on_negative_cancel_without_crosses` (T08).
- **X01/X02 demoted to plain positives.** `spawn_on_positive_async_crosses.sg`
  and the other positives keep their matrix-documented filenames even though the
  `crosses` keyword is gone from their bodies (naming drift retained
  intentionally so the matrix cross-reference stays stable).
- **S06 (`spawn distributed { ret 1; }`, `SEM3111`)** is framed in statement
  position (return position parses the trailing block as a struct literal and
  yields `SYN2012`/`SEM3051` instead). The faithful orphaned-block form emits
  the documented three-diagnostic recovery cascade: `SEM3111` (spawn of a
  `Placement`, not a `Task`) + `SEM3134` (`ret` outside a value block) + `SYN2012`
  (statement recovery).
- **Nested crossings.** A uniform `SemaOnNested` (`SEM3153`) rejects any crossing
  (`on` or `spawn on`) opening another crossing block; the matrix has no nested
  spawn-on row, so this is a conservative superset.
- **Backend guard is one-per-construct.** End-to-end (`surge build --backend vm`
  and `--backend llvm`), each remote-spawn/await/cancel construct emits exactly
  one `FUT7015`/`FUT7016`/`FUT7017`; two spawn-ons emit two. Span-dedup is
  correct, matching Block 2's landed behavior. The higher counts visible in the
  `crossinggate` `diagnoseBackend` harness come only from compiling the whole
  `block03/invalid/` directory as a single module (one guard per sibling
  construct), not from duplication; the harness asserts code presence, so
  activation is unaffected.
- **Sidecar diagnostic counts.** Most negatives emit a single diagnostic; three
  carry a documented two-diagnostic cascade within Block 2 precedent
  (`local_task_assignment`: `SEM3015`+`SEM3107`; `missing_block`:
  `SYN2032`+`SEM3160`; `pinned_capture`: `SEM3005`+`SEM3167`, mirroring block02
  `on_negative_shard_pinned_capture`).

## Accepted Surface

`spawn on dst { ... }` creates placed work and returns a `far Task<T>` handle.
It does not wait for completion immediately.

Accepted shape:

```sg
fn start(job: own Job) crosses -> far Task<Result> {
    return spawn on distributed {
        ret run_job(own job);
    };
}
```

Rules imported from Epic 11:

- `dst` must be a `Placement` destination.
- `far` handle destinations are invalid in Block 3.
- `@local spawn on dst` is invalid.
- `blocking` as a destination is invalid and postponed.
- `ret expr;` produces the remote task result.
- `return` inside the remote spawn block is invalid.
- Captures fall into the three accepted categories: `Copy` values (including
  `Placement`), `own @shard_movable` values, and `far T` handles by move
  (affine). A local `Task<T>` is not `@shard_movable`.
- The expression result type is `far Task<T>`, which is strictly affine and
  follows local `Task<T>` must-await-or-return lifecycle rules.
- Use of `spawn on` requires an enclosing `crosses` function or block context
  once Block 4 crossing checks are active.

## Destination Typing Matrix

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| D01 | `spawn on distributed { ret value; }` | Valid | Destination is prelude `Placement`; result is `far Task<T>`. |
| D02 | `spawn on pool { ret value; }` | Valid | Destination is prelude `Placement`; result is `far Task<T>`. |
| D03 | `spawn on shard(id) { ret value; }` | Valid | `shard(id)` returns `Placement`; result is `far Task<T>`. |
| D04 | `spawn on dst { ret value; }` where `dst: Placement` | Valid | Computed placement variable accepted. |
| D05 | `spawn on route_for(id) { ret value; }` where `route_for(...) -> Placement` | Valid | Placement-returning call accepted. |
| D06 | `spawn on 1 { ret value; }` | Invalid | Diagnostic: `SEM3154`; fix: use `pool`, `distributed`, `shard(id)`, or a `Placement` value. |
| D07 | `spawn on Job { ret value; }` where `Job` is a type | Invalid | Diagnostic: `SEM3155`; fix: pass a `Placement` value, not a type. |
| D08 | `spawn on route_for { ret value; }` with bare function name | Invalid | Diagnostic: `SEM3156`; fix: call the function if it returns `Placement`. |
| D09 | `spawn on route_for(id) { ret value; }` where return type is not `Placement` | Invalid | Diagnostic: `SEM3154`; fix: make the function return `Placement` or choose a placement value. |
| D10 | `spawn on ch { ret value; }` where `ch: far Channel<T>` | Invalid | Diagnostic: `SEM3157`; fix: use `on ch { ... }` for immediate owner-anchored handle operations. |
| D11 | `spawn on conn { ret value; }` where `conn: far TcpConn` | Invalid | Diagnostic: `SEM3157`; fix: use an accepted `Placement` destination. |
| D12 | `spawn on t { ret value; }` where `t: far Task<T>` | Invalid | Diagnostic: `SEM3158`; fix: call `t.await()` or `t.cancel()` in a `crosses` context. |
| D13 | `spawn on blocking { ret value; }` | Invalid | Diagnostic: `FUT7013` (postponed; reuse `FUT`-range blocking code); fix: use `blocking { ... }` as the existing local blocking-task form, or use a `Placement` destination. |

## Syntax Matrix

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| S01 | `spawn on distributed { ret value; }` | Valid | Parser recognizes `spawn on` as remote-spawn expression. |
| S02 | `let t = spawn on pool { ret value; };` | Valid | Remote-spawn expression allowed in expression position. |
| S03 | `return spawn on distributed { ret value; };` | Valid | Remote-spawn expression allowed in return expression position. |
| S04 | `spawn on distributed;` | Invalid | Diagnostic: `SYN2032`; fix: add `{ ret expr; }`. |
| S05 | `spawn on { ret value; }` | Invalid | Diagnostic: `SYN2033`; fix: add a `Placement` destination. |
| S06 | `spawn distributed { ret value; }` | Invalid via existing local `spawn` grammar | `spawn` consumes one postfix expression (`distributed`), leaving an orphaned block. With `distributed` declared as a `Placement` value (not a `Task`), the deterministic existing diagnostic is `SemaSpawnNotTask` (3111, "spawn requires async function call or Task<T> expression"), reused; this is not a new missing-`on` code. Fix: write `spawn on distributed { ... }`. |
| S07 | `@local spawn on distributed { ret value; }` | Invalid | Diagnostic: `SEM3174` (Block 4); fix: remove `@local` or use local `@local spawn expr`. |

## Body Result Matrix

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| B01 | `spawn on pool { ret 1; }` | Valid | Result type is `far Task<int>`. |
| B02 | `spawn on pool { let x = 1; ret x; }` | Valid | Statements before final `ret` are accepted. |
| B03 | `spawn on pool { ret nothing; }` | Valid | Result type is `far Task<nothing>`. |
| B04 | `spawn on pool { return 1; }` | Invalid | Diagnostic: `SEM3159`; fix: replace `return` with `ret`. |
| B05 | `spawn on pool { let x = 1; }` | Invalid | Diagnostic: `SEM3160`; fix: add explicit `ret expr;`. |
| B06 | `spawn on pool { ret 1; ret 2; }` | Invalid | Diagnostic: `SEM3161`; fix: keep one reachable `ret`. |
| B07 | `spawn on pool { ret local_task.await(); }` capturing a local `Task<T>` | Invalid (capture violation) | Capturing a local `Task<T>` into a remote body is a capture violation, not an await error: `Task<T>` is not `@shard_movable`. Diagnostic: `SEM3168` (Block 4); fix: keep the local task on its owner shard, or pass a shard-movable value. |

## Capture And Sendability Matrix

| ID | Capture | Status | Expected result |
| --- | --- | --- | --- |
| C01 | Capture primitive `Copy` value | Valid | Value may be copied into remote task. |
| C02 | Capture `own Job` where `Job` is `@shard_movable` | Valid | Owned shard-movable value may cross. |
| C09 | Capture a `far T` handle by move (affine) | Valid | The local handle is moved into the remote task; the remote resource is not moved. |
| C10 | Capture or compute a `Placement` value | Valid | `Placement` is `Copy` and shard-movable, so it may be captured or produced by `shard(id)` inside the body. |
| C03 | Capture `Job` by shared borrow `&Job` | Invalid | Diagnostic: `SEM3165` (Block 4); fix: move an owned shard-movable value or copy a Copy value. |
| C04 | Capture `Job` by mutable borrow `&mut Job` | Invalid | Diagnostic: `SEM3165` (Block 4); fix: move ownership instead. |
| C05 | Capture `own Job` where `Job` lacks `@shard_movable` | Invalid | Diagnostic: `SEM3168` (Block 4); fix: mark the type `@shard_movable` if valid or pass a remote handle/value summary. |
| C06 | Capture `own LocalOnly` where `LocalOnly` is `@nosend` | Invalid | Diagnostic: `SEM3166` (Block 4); fix: remove the capture, use local `@local spawn`, or pass a sendable/shard-movable value. |
| C07 | Capture `own Conn` where `Conn` is `@shard_pinned` | Invalid | Diagnostic: `SEM3167` (Block 4); fix: use a `far Conn` handle where an accepted operation exists, or keep work on the owner shard through Block 2 `on`. |
| C08 | Capture `own SendOnly` where `SendOnly` is `@send` but not `@shard_movable` | Invalid | Diagnostic: `SEM3169` (Block 4); fix: add `@shard_movable` only if the type satisfies shard-movement rules. |

## Crosses Propagation Matrix

**RETIRED (design change).** The explicit `crosses` keyword is being removed;
the crossing effect is inferred into metadata rather than required. X03/X04 no
longer produce diagnostics, and their negative fixtures were deleted. X01/X02
are demoted to plain positives (they parse and type-check with `crosses` absent).

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| X01 | `fn f(...) -> far Task<T> { return spawn on pool { ret value; }; }` | Valid (plain positive) | Crossing effect inferred; no keyword required. |
| X02 | `async fn f(...) -> far Task<T> { return spawn on pool { ret value; }; }` | Valid (plain positive) | `async` combines with an inferred crossing effect. |
| X03 | `fn f(...) -> far Task<T> { return spawn on pool { ret value; }; }` | RETIRED | Was `SEM3162`; crossing now inferred, no diagnostic. Fixture deleted. |
| X04 | Caller invokes a crossing function returning `far Task<T>` from a non-crossing function | RETIRED | Was `SEM3163`; crossing now inferred, no diagnostic. Fixture deleted. |

## `far Task<T>` Identity And Operation Matrix

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| T01 | `let t: far Task<int> = spawn on distributed { ret 1; };` | Valid | Remote spawn expression has type `far Task<int>`. |
| T02 | `let t: Task<int> = spawn on distributed { ret 1; };` | Invalid | Diagnostic: `SEM3015`; fix: change annotation to `far Task<int>` or use local `spawn`. |
| T03 | `return spawn on distributed { ret value; };` from `crosses -> far Task<T>` | Valid | Return type matches `far Task<T>`. |
| T04 | `return spawn on distributed { ret value; };` from `crosses -> Task<T>` | Invalid | Diagnostic: `SEM3015`; fix: return `far Task<T>` or use local `spawn`. |
| T05 | `t.await()` where `t: far Task<T>` inside `crosses` function | Valid | Consumes the handle (affine) and returns `TaskResult<T>`. |
| T06 | `t.cancel()` where `t: far Task<T>` inside `crosses` function | Valid | Consumes the handle (affine) and returns `TaskResult<nothing>`. |
| T07 | `t.await()` where `t: far Task<T>` inside a non-crossing function | RETIRED | Was `SEM3164`; crossing now inferred, no diagnostic. Fixture deleted. |
| T08 | `t.cancel()` where `t: far Task<T>` inside a non-crossing function | RETIRED | Was `SEM3164`; crossing now inferred, no diagnostic. Fixture deleted. |
| T09 | `on t { ret value; }` where `t: far Task<T>` | Invalid by Block 2 dependency | Diagnostic: `SEM3143` (Block 2); fix: use `t.await()` or `t.cancel()`. |
| T10 | `let r: TaskResult<T> = t.await();` where `t: far Task<T>` in `crosses` | Valid | Result identity matches local `Task<T>.await()` shape but crosses remotely. |
| T11 | `let r: T = t.await();` where `t: far Task<T>` | Invalid | Diagnostic: `SEM3015`; fix: handle `TaskResult<T>`. |
| T12 | `let r: nothing = t.cancel();` where `t: far Task<T>` | Invalid | Diagnostic: `SEM3015`; fix: handle `TaskResult<nothing>`. |

## `far Task<T>` Lifecycle Matrix

`far Task<T>` is strictly affine and mirrors local `Task<T>` lifecycle rules.
The infrastructure map confirms a local `Task<T>` dropped without `.await()` or
return is a compile-time error (`SemaTaskNotAwaited` 3107), not a detach or
implicit cancel; `far Task<T>` follows the same must-await-or-return rule. Both
`.await()` and `.cancel()` consume the handle (own `self`), so any second use is
a use-after-move resolved statically.

| ID | Form | Status | Expected result |
| --- | --- | --- | --- |
| L01 | `let t = spawn on pool { ret 1; };` with `t` unused at end of scope | Invalid | A `far Task<T>` must be awaited or returned. Diagnostic: reuse the local-task not-awaited diagnostic (`SemaTaskNotAwaited` 3107). Fix: `t.await()`, `t.cancel()`, or return `t`. |
| L02 | `let a = t.await(); let b = t.await();` | Invalid | `.await()` consumed the affine handle; the second use is a use-after-move. Diagnostic: reuse `SemaUseAfterMove` (3130). Fix: await once. |
| L03 | `t.cancel(); let r = t.await();` | Invalid | `.cancel()` consumed the handle; the following `.await()` is a use-after-move. Diagnostic: reuse `SemaUseAfterMove` (3130). Fix: do not use the handle after cancel. |
| L04 | `return t;` from `crosses -> far Task<T>` | Valid | Returning the handle transfers ownership; it is not a drop. |

## Backend And Feature-Gate Matrix

If a backend, build configuration, or feature gate accepts parsing and semantic
analysis but cannot lower or execute Phase 4 remote spawn yet, the compiler must
emit a deterministic diagnostic instead of crashing, panicking, leaking, or
falling through to an ambiguous backend error.

| ID | Condition | Status | Expected result |
| --- | --- | --- | --- |
| G01 | Phase 4 remote spawn lowering unavailable | Invalid for that configuration | Diagnostic: `FUT7015` (`FUT` range); fix: enable a backend/configuration that supports Phase 4 remote spawn when available. |
| G02 | `far Task<T>.await()` lowering unavailable | Invalid for that configuration | Diagnostic: `FUT7016` (`FUT` range); fix: use a supported backend/configuration when available. |
| G03 | `far Task<T>.cancel()` lowering unavailable | Invalid for that configuration | Diagnostic: `FUT7017` (`FUT` range); fix: use a supported backend/configuration when available. |

## Positive Golden Fixture Inventory

| Fixture | Matrix rows |
| --- | --- |
| `spawn_on_positive_distributed.sg` | D01, S01, B01, X01, T01 |
| `spawn_on_positive_pool.sg` | D02, S02, B01 |
| `spawn_on_positive_shard.sg` | D03 |
| `spawn_on_positive_placement_var.sg` | D04 |
| `spawn_on_positive_route_fn.sg` | D05 |
| `spawn_on_positive_return_far_task.sg` | S03, T03, L04 |
| `spawn_on_positive_ret_nothing.sg` | B03 |
| `spawn_on_positive_copy_capture.sg` | C01 |
| `spawn_on_positive_shard_movable_capture.sg` | C02 |
| `spawn_on_positive_far_handle_capture.sg` | C09 |
| `spawn_on_positive_placement_capture.sg` | C10 |
| `spawn_on_positive_async_crosses.sg` | X02 |
| `spawn_on_positive_far_task_await_crosses.sg` | T05, T10 |
| `spawn_on_positive_far_task_cancel_crosses.sg` | T06 |

## Negative Golden Fixture Inventory

Every negative fixture asserts an exact diagnostic code. Historical `TBD-DIAG-*`
placeholders were resolved in `11-tasks/README.md` before implementation.

| Fixture | Matrix rows | Diagnostic code | Fix availability |
| --- | --- | --- | --- |
| `spawn_on_negative_nonplacement_literal.sg` | D06 | `SEM3154` | Fixable: use `Placement`. |
| `spawn_on_negative_type_destination.sg` | D07 | `SEM3155` | Fixable: use a value destination. |
| `spawn_on_negative_bare_function_destination.sg` | D08 | `SEM3156` | Fixable: call the function. |
| `spawn_on_negative_nonplacement_route_fn.sg` | D09 | `SEM3154` | Fixable: return `Placement` or choose another destination. |
| `spawn_on_negative_far_channel_destination.sg` | D10 | `SEM3157` | Fixable: use Block 2 `on ch { ... }` for immediate operations. |
| `spawn_on_negative_far_tcp_destination.sg` | D11 | `SEM3157` | Fixable: use accepted `Placement`. |
| `spawn_on_negative_far_task_destination.sg` | D12 | `SEM3158` | Fixable: use `await()` or `cancel()`. |
| `spawn_on_negative_blocking_destination.sg` | D13 | `FUT7013` (reuse `FUT`-range blocking code) | Fixable: separate local `blocking {}` from remote placement. |
| `spawn_on_negative_missing_block.sg` | S04 | `SYN2032` | Fixable: add block. |
| `spawn_on_negative_missing_destination.sg` | S05 | `SYN2033` | Fixable: add destination. |
| `spawn_on_negative_missing_on.sg` | S06 | reuse `SemaSpawnNotTask` (3111); existing local `spawn` grammar path, not a new code | Fixable: add `on` for remote spawn or use local `spawn expr`. |
| `spawn_on_negative_local.sg` | S07 | `SEM3174` (Block 4) | Fixable: remove `@local` or use local spawn. |
| `spawn_on_negative_return_inside.sg` | B04 | `SEM3159` | Fixable: replace with `ret`. |
| `spawn_on_negative_missing_ret.sg` | B05 | `SEM3160` | Fixable: add `ret expr;`. |
| `spawn_on_negative_unreachable_ret.sg` | B06 | `SEM3161` | Fixable: remove unreachable result path. |
| `spawn_on_negative_local_task_capture.sg` | B07 | `SEM3168` (Block 4) | Fixable: keep the local task on its owner shard or pass a shard-movable value. |
| `spawn_on_negative_borrow_capture.sg` | C03 | `SEM3165` (Block 4) | Fixable: move owned shard-movable value or copy value. |
| `spawn_on_negative_mut_borrow_capture.sg` | C04 | `SEM3165` (Block 4) | Fixable: move ownership. |
| `spawn_on_negative_non_movable_capture.sg` | C05 | `SEM3168` (Block 4) | Fixable only if type can validly become `@shard_movable`. |
| `spawn_on_negative_nosend_capture.sg` | C06 | `SEM3166` (Block 4) | Fixable: use local `@local spawn` or remove capture. |
| `spawn_on_negative_pinned_capture.sg` | C07 | `SEM3167` (Block 4) | Fixable: use remote handle or owner-shard operation. |
| `spawn_on_negative_send_not_shard_movable.sg` | C08 | `SEM3169` (Block 4) | Fixable only if shard-movement rules are satisfied. |
| ~~`spawn_on_negative_without_crosses.sg`~~ | X03 | RETIRED (`SEM3162`) | Deleted: `crosses` inferred, requirement removed. |
| ~~`spawn_on_negative_crosses_call_propagation.sg`~~ | X04 | RETIRED (`SEM3163`) | Deleted: `crosses` inferred, requirement removed. |
| `spawn_on_negative_local_task_assignment.sg` | T02 | `SEM3015` | Fixable: use `far Task<T>` annotation or local `spawn`. |
| `spawn_on_negative_return_local_task_mismatch.sg` | T04 | `SEM3015` | Fixable: return `far Task<T>` or use local `spawn`. |
| ~~`spawn_on_negative_await_without_crosses.sg`~~ | T07 | RETIRED (`SEM3164`) | Deleted: `crosses` inferred, requirement removed. |
| ~~`spawn_on_negative_cancel_without_crosses.sg`~~ | T08 | RETIRED (`SEM3164`) | Deleted: `crosses` inferred, requirement removed. |
| `spawn_on_negative_await_result_mismatch.sg` | T11 | `SEM3015` | Fixable: handle `TaskResult<T>`. |
| `spawn_on_negative_cancel_result_mismatch.sg` | T12 | `SEM3015` | Fixable: handle `TaskResult<nothing>`. |
| `spawn_on_negative_far_task_dropped.sg` | L01 | reuse `SemaTaskNotAwaited` (3107) | Fixable: await, cancel, or return the handle. |
| `spawn_on_negative_double_await.sg` | L02 | reuse `SemaUseAfterMove` (3130) | Fixable: await once. |
| `spawn_on_negative_await_after_cancel.sg` | L03 | reuse `SemaUseAfterMove` (3130) | Not safely fixable automatically: remove the post-cancel use. |
| `spawn_on_negative_backend_unavailable.sg` | G01 | `FUT7015` | Fixable only by selecting/enabling a supported backend/configuration. |
| `spawn_on_negative_await_backend_unavailable.sg` | G02 | `FUT7016` | Fixable only by selecting/enabling a supported backend/configuration. |
| `spawn_on_negative_cancel_backend_unavailable.sg` | G03 | `FUT7017` | Fixable only by selecting/enabling a supported backend/configuration. |

## Follow-Up Test Surfaces

After the golden fixture matrix exists and diagnostics are allocated, Block 3
implementation follow-up must add focused tests in these surfaces:

- Parser/AST tests proving `spawn on` is parsed as remote spawn and not as
  local `spawn` plus a separate `on` expression.
- Parser/AST tests for destination/block presence and keyword ordering.
- A test proving `spawn distributed { ... }` (S06) resolves through the existing
  local `spawn` grammar to `SemaSpawnNotTask` (3111), not a new missing-`on`
  diagnostic.
- Semantic tests for `Placement` destination typing.
- Semantic tests for capture/sendability and `@shard_movable` requirements,
  including the local-`Task<T>` capture violation (B07).
- Semantic tests for `crosses` propagation on `spawn on`, `far Task<T>.await()`,
  and `far Task<T>.cancel()`.
- Semantic tests proving `far Task<T>` is distinct from local `Task<T>`.
- Semantic tests for `far Task<T>` affine lifecycle: drop-without-await
  (`SemaTaskNotAwaited`), double-await, and await-after-cancel
  (`SemaUseAfterMove`).
- Lowering tests for the minimal accepted remote-spawn path once the backend is
  available.
- Lowering diagnostics tests for deterministic backend-unavailable behavior.
