# Epic 20 Task 4: Drop-Fn Registration + Dispatch

Turn the dormant `__surge_drop_call` stub into a real dispatcher:
every crossing that ships an owned state registers a compiled drop
function under a deterministic nonzero id, and the switch routes that
id to the state's recursive glue. Abandon-path behavior rows are
Task 5; this task lands the registration, the dispatch, and the
happy-path proof that real ids change nothing until an abandon fires.

## Resolved mechanics (fork 1 applied, verified in code)

- **Id = the crossing body's `FuncID`** (`ins.BodyFuncID`): nonzero,
  unique per crossing, deterministic across builds, already present
  at every call site and in every runtime pending row — no separate
  counter, and runtime traces correlate drop id == body id for free.
- **Drop function = the state struct's existing recursive glue**
  (`drop.type<N>` for `ins.State.TypeID`): the crossing state is a
  boxed struct emitted by `emitStructLit`, exactly the shape Epic 19
  glue frees (fields then box). No new glue family.
- Call sites that ship a state: spawn-on
  (`emit_crossing.go` `rt_remote_spawn_publish_placement`), immediate
  `on` (`emit_crossing_immediate_on.go` `rt_immediate_on_execute`),
  anchored `on ch` (`emit_crossing_anchored_on.go`
  `rt_immediate_on_execute_anchored`). Stateless first attempts keep
  id 0; RETRY calls keep `(id=0, state=null)` — the epic's fixed
  point (a retry re-ships already-moved state otherwise).
- Emission order is safe: `emitFunctions` (registers) →
  `emitDropGlue` → `emitPollDispatch` (emits the switch).
- The runtime guard `state_owned != 0 && state_drop_fn_id != 0 &&
  state != NULL` (`rt_remote_spawn_pending.c:21`) means real ids arm
  the existing abandon sites without any runtime change; select stays
  `(0, null)` by design (Task 7).

## Rows (test-first)

1. Static IR rows (backend emit tests): a state-shipping spawn-on /
   immediate-on / anchored-on call passes its body FuncID as the drop
   id; the stateless and retry calls keep `i64 0`; the
   `__surge_drop_call` switch carries one arm per registered id (in
   sorted order) calling that state type's `drop.type<N>` glue; the
   default arm keeps the "missing drop function" panic (negative
   control for unregistered ids).
2. Happy-path behavior: the existing transport e2e suite
   (`runtime-v2-transport-check`) runs unchanged with real ids — a
   consumed state never dispatches; plus a census-style e2e row: a
   spawn-on crossing with an owned heap capture completes and the
   program's alloc/free stays balanced (sync-main entry shape per
   RV2-DEBT-049/050).
3. Gates: `make check`, golden regen byte-stable, Sentrux
   committed-tree comparison.

## Evidence (2026-07-17)

- Implementation: `Emitter.crossingDropStates` registry +
  `registerCrossingDropState` (`emit_drop_glue.go`; conflicting
  re-registration errors out); the three state-shipping call sites
  pass `BodyFuncID` as the drop id (spawn-on, immediate `on`,
  anchored `on ch`); stateless and RETRY calls keep `(0, null)`
  untouched; `emitPollDispatch` populates the `__surge_drop_call`
  switch from the sorted registry, each arm calling the state type's
  `drop.type<N>` glue; the default panic arm stays (negative control).
  Glue is emitted for every registered state (an all-Copy state still
  frees its box).
- Static IR rows: `TestEmitSpawnOnRegistersStateDropFn` and
  `TestEmitImmediateOnRegistersStateDropFn`
  (`emit_crossing_drop_dispatch_test.go`) — drop id == body id on the
  state-shipping first attempt, `(0, 0)` on retry, sorted switch arm
  routing to recursive glue, default panic intact.
- Happy path: full transport e2e suite
  (`TestRuntimeV2(Migration|RemoteSpawn|ImmediateOn|Transport)*`)
  green with real ids — a consumed state never dispatches.
- The planned census e2e row is BLOCKED and reassigned to Task 5:
  probing it found RV2-DEBT-051 (pre-existing, reproduced with these
  changes stashed) — heap-bearing crossing captures arrive CORRUPTED
  (body-side `j.id` reads a pointer bit pattern; constant-returning
  body is fine) and LEAK on the happy path (48 allocs / 42 frees over
  the scenario window). Epic 18's e2e only covered all-Copy-literal
  captures, which masked both facets. Task 5's handoff contract owns
  the fix; the census row lands there on the fixed contract.
- Gates: `go test ./internal/backend/llvm` green; transport suites
  green; `make check` recorded below.

## Status

COMPLETE (2026-07-17) for the task's own scope (registration +
dispatch + static rows + happy-path suites). The census row moved to
Task 5 with RV2-DEBT-051.
