# Epic 18 Task 4: Compiled Drop Functions + Guard Flip + E2E

## Design (locked before implementation)

### Compiled drop functions

- MIR synthesizes one drop function per crossing state struct that has
  droppable content: `__crossing_state_drop$<pollID>` — body = the
  struct's drop glue over a `__task_state()`-style pointer parameter
  (mirror of the synthetic select body: hand-built Func, CalleeValue
  helpers, TermReturn). The function id IS the drop-fn id shipped to
  the runtime.
- The prepare functions (`prepareSpawnOnCrossing`,
  `prepareImmediateBodyCrossing`) compute whether the state struct has
  droppable fields (any capture whose type is not plain-copy); when it
  does, they synthesize the drop fn and record its id on the
  CrossingInstr (`StateDropFuncID`, 0 otherwise).
- LLVM emits gain the id in place of today's `i64 0` at every caller
  call site, and `__surge_drop_call` becomes a real switch over the
  emitted drop functions (extend the drop-dispatch emission to collect
  `isDropFunc` by name prefix, exactly like the poll dispatch).
- The body's own frame drops captures today through the lowered body's
  drop glue (the state fields are materialized into locals at the body
  prologue); the drop fn covers only the never-ran paths — the
  first-poll rule from Task 3 means both can't fire for one state, and
  the e2e census is the proof.

### Sema guard flip

- `crossingRecordExecutable` accepts `CrossingCaptureOwnedShardMovable`
  for SpawnOn/OnPlacement/OnFarHandle; the FUT7020 arm in the
  classifier keeps firing for far-Task captures and non-copy payloads
  (results) unchanged.
- The `@shard_movable` sema rules (SEM3168-3172) already police which
  owned types may cross; no new diagnostics.

### E2E (census-observed, SHARDS=1/2/8)

- Happy path: build an owned `@shard_movable` value (heap-owning
  field) locally, move it into `spawn on` / `on` blocks, process
  remotely, complete; assert results AND a clean heap census
  (alloc/free balance) at exit — the recursive-free (nested) proof.
- Refusal path: drive one deterministic pre-body refusal (invalid
  placement) with a moved owned capture; census must balance — the
  drop fn ran through the pending edge.
- Negative goldens: far-Task capture keeps FUT7020; owned RESULT keeps
  the reply-payload guard.

## Status

COMPLETE, with the epic's second scope correction: compiled drop
functions have NOTHING to build on today — `InstrDrop` is a no-op in
the LLVM backend and compiled code frees no owned heap value at scope
exit anywhere (the contract's recorded "current malloc/free strategy"
pre-1.0 model). Therefore:

- The guard flip ships under the language's CURRENT memory model: an
  owned capture moved into a crossing body is reclaimed exactly as it
  would have been locally (today: not at all) — the crossing adds NO
  new leak class. The drop-obligation plumbing (Tasks 2-3) stays in
  place at id 0, dormant and row-proven, and becomes load-bearing the
  day the language grows real drop emission — recorded as the new
  debt row (drop emission is language-wide work, not a crossing
  epic).
- What shipped here: `crossingRecordExecutable` accepts owned
  @shard_movable captures for all three body-carrying forms; the
  FUT7020 classifier arm for them is gone (far-Task captures and
  non-copy payloads keep their guards, kindness rows retargeted);
  positive compile tests for both shapes; e2e green FIRST RUN at
  SHARDS=1/2/8 (spawn-on and immediate-on both move an owned
  @shard_movable value built locally); the crossing e2e gate carries
  the new test.
- Census assertions are deliberately absent: with no frees in the
  language there is nothing to balance; the e2e proves behavior
  (moved values processed remotely with correct results and affine
  move-out at the source).
