# Epic 18 Task 1: Kickoff — Evidence, Drop-Metadata Design, Sema Surface

Direction approved 2026-07-13 (Model A per the epic doc's fork
resolution; the review's three corrections — pending drop-metadata
plumbing, terminal-refusal row premise, 21-row matrix on the
live-remote-owner axis — are folded into the epic doc).

## Evidence Re-Pin (verified at commit `3f769404`)

The guard to lift (exactly three sites):

- Verdict: `CrossingCaptureOwnedShardMovable`
  (`internal/sema/on_crossing_capture.go:198` returns it for owned
  `@shard_movable` captures).
- Diagnostic: FUT7020 arm in
  `internal/buildpipeline/crossing_guard_classify.go` ("moves owned
  data across shards, which this vertical does not ship yet").
- Executability: `crossingRecordExecutable`
  (`internal/buildpipeline/crossing_transport.go:87`) returns false on
  the verdict.

The hole the epic opens and must close in the same motion:

- `rt_remote_spawn_pending` carries `uint64_t poll_fn_id; void* state;`
  with NO drop metadata (`rt_remote_spawn_internal.h:7-21`); the
  abandon paths free only the envelope
  (`rt_remote_spawn_pending.c` consume/release).
- `rt_remote_task_pending` likewise: `body_poll_fn_id`/`body_state`
  with no drop metadata (`rt_remote_task_internal.h`) — the immediate
  `on` / anchored `on ch` / remote-select families share the gap.
- No drop dispatch exists in the compiled ABI: the runtime can call
  `__surge_poll_call(id)` but has NO way to destruct a state struct
  without running its body.

Existing machinery to mirror:

- `__surge_poll_call` is a compiler-emitted switch over poll functions
  (`internal/backend/llvm/emit_async.go:31-74`) — the exact template
  for a drop dispatch.
- MIR has `InstrDrop` and drop-glue emission for owned locals; the
  crossing state struct is built by `buildSpawnOnStateStruct`
  (`internal/mir/lower_expr_crossing_spawn_poll.go`).
- The exactly-once-reply discipline (Epics 13-17) is the proof shape;
  the two Epic 17 cancel-gate defects are the cautionary record (every
  op-tagged sweep must know every op).

## Drop-Metadata Design (locked)

- **ABI**: the compiler emits `__surge_drop_call(i64 drop_fn_id,
  ptr state)` — a switch over per-state-struct drop functions,
  mirroring `__surge_poll_call`. A drop function exists only for state
  structs with droppable content; id 0 means "nothing to drop" and is
  the universal default (today's plain-copy states keep id 0, so the
  change is behavior-neutral until sema admits droppable captures).
- **Pendings carry the id**: `rt_remote_spawn_pending` and
  `rt_remote_task_pending` gain `uint64_t state_drop_fn_id` filled at
  request creation alongside `state`/`body_state`.
- **The handoff rule (single owner at every instant)**: the drop
  obligation travels WITH the state pointer.
  - From request creation until the body task is PUBLISHED, the
    pending owns it: every terminal edge that abandons the pending
    without a published body calls `__surge_drop_call` exactly once
    (matrix rows 1-9; the runtime nulls `state` after dropping so a
    double-visit is structurally impossible).
  - From publish on, the BODY TASK owns it (rows 10-18): normal
    completion drops through the body's own drop glue; the
    cancelled-before-first-poll and teardown-with-parked-body edges
    drop through the task's carried drop id at task teardown — the
    task struct gains the same `state_drop_fn_id` so no path needs to
    find the pending again.
  - The publish edge itself is the row-13/14 boundary: dispatch-side
    failures after state arrival but before a live task drop
    dispatch-side.
- **Nested exactly-once (row 19)** is the drop function's own
  property: the compiler emits it from the struct's drop glue, which
  recursively drops owned fields — the matrix row proves the runtime
  calls it exactly once, and the glue's recursion does the rest.

## Sema Surface Design (implemented in Task 4)

- Flip: `crossingRecordExecutable` accepts
  `CrossingCaptureOwnedShardMovable` for `on`/`spawn on`/`on ch`
  forms; the FUT7020 classifier arm keeps firing for the verdicts
  that remain unshippable (far Task captures) and for non-movable
  payloads (unchanged).
- Move semantics at the capture site are already policed
  (use-after-move on owned captures exists); no new diagnostics are
  expected for the happy path. Kindness rows: non-movable field paths
  keep the `NonCopyCulpritPath` naming; owned RESULTS keep their
  existing reply-payload guard with its build-on-destination hint.
- Affine fixed point (approved): a move is terminal on every outcome
  including failures; no un-move. A fallible-move surface is a
  recorded tail, not this epic.

## Baselines (Sentrux, committed tree `3f769404`)

| Scope | Quality |
| --- | --- |
| `.` (root, advisory) | 6173 |
| `internal` | 6489 |
| `runtime` | 5297 |
| `runtime/native` | 5384 |

## Open Debt Touched

None of RV2-DEBT-031/032/033 is touched. Task 2 must state its
position against the pending-family file caps (the remote-task family
gate) when adding the drop-fn field and abandon-path drops.
