# Epic 17 Task 4: Sema + Lowering + Capability + E2E

## Design (locked before implementation)

### Sema surface

Today a far-channel arm in `select` is a plain type mismatch ("recv
expects Channel<T>"): `isSelectAwaitableExpr` / `typeSelectAwaitExpr`
(`internal/sema/type_expr_select.go`) accept only local channel and
task shapes. The vertical adds:

- Far arm recognition: `recv`/`send` members whose receiver is
  `far Channel<T>` (`tc.farInner` + `tc.channelPayloadType`, the same
  evidence `on ch` routing uses in `type_expr_calling.go`).
- The vertical-1 shape rule, SEM3176 `SemaSelectFarArmsSingleOwner`:
  a select containing ANY far arm must contain ONLY far channel arms —
  no local channel arms, no task arms, no timeout arms, no default arm
  (the whole select ships as one anchored block in this vertical; the
  timeout/default caller-side composition is the stabilization slice's
  seam). The diagnostic names the restriction and the workaround:
  split into one select per owner / keep local arms in a local select.
- Owner-shard sameness is NOT compile-time provable (owner shard is
  runtime data in the far token); sema enforces the type-visible rule
  and the RUNTIME enforces same-owner at the call (INVALID_ARGUMENT
  from `rt_far_channel_select`), which the lowering maps to a
  kindness-first panic naming the split-into-selects rewrite. This is
  the recorded "multi-owner selects deferred" boundary.
- Crossing record: `CrossingLoweringChannelSelect` appended at the
  enum END; ResultType = the select's result type; payload = the
  winner index (always plain-copy); SuspendCapable follows awaitDepth
  (select already demands async context). FUT7022
  `FutChannelSelectBackendUnavailable` through the shared classifier
  (sync-context 7019 and payload 7020 causes rank first, as
  everywhere).

### Lowering

- MIR: `lowerSelectExpr` detects the far select via the sema crossing
  record on the select expression and swaps `InstrSelect` for the
  crossing instr (pending + handle-array slots); the winner-index arm
  dispatch (`select_index` compare chain) is REUSED unchanged after
  the crossing's ready edge.
- The body poll fn is ONE canonical synthetic function per module
  (`rt_anchored_channel_select()` + async-return of the winner) — the
  compiled twin of the harness body; every remote select in the module
  shares its poll id.
- LLVM: `emitChannelSelectCrossing` builds the anchors/kinds/bits
  arrays, calls `rt_far_channel_select`, retries through the pending
  (arm arrays are only read on the first call), and maps failure
  statuses to deterministic panics — INVALID_ARGUMENT names the
  single-owner restriction and the per-owner split rewrite.
- Capability: `ChannelSelect` joins backendSupportsCrossingForm /
  crossingFormsForRequest / crossingRecordExecutable (executable —
  only the winner index rides the reply) in
  `internal/buildpipeline/crossing_transport.go`; forms table gains
  FUT7022.

### E2E (SHARDS=1,2,8)

Fan-in: N producers over `share()` siblings send to two channels on
one owner shard; the selector loops remote selects and drains both;
the winner-index contract (readiness select, values drained by
followup anchored recv) mirrors the local select surface. Negative
diagnostics goldens: mixed local+far arm (SEM3176), sync context
(FUT7019), off-LLVM backend (FUT7021-style 7022).

## Status

Design locked; implementation follows as increments (sema surface ->
lowering -> capability+e2e), each behind `make check`.
