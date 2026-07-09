# Epic 13 Task 5: Placement ABI And Destination Resolution

**Status:** complete (2026-07-09).
**Kind:** runtime ABI + prelude/backend plumbing.
**Depends on:** Task 1 (placement decisions). May run parallel to Tasks 3-4.

## Goal

Give `Placement` a real runtime representation and destination resolution for
`shard(id)` and `distributed`, per the Task 1 decisions. `pool` remains a
deterministic diagnostic. No crossing executes yet — this task ends at "a
compiled program can materialize a Placement value and the runtime can
resolve it to a destination shard id, with trace evidence".

## Starting State (verify and re-pin)

- `core/intrinsics.sg:193-206`: `pub type Placement = { __opaque: ... }`
  (Copy, shard-movable), `@intrinsic pub const pool: Placement`,
  `@intrinsic pub const distributed: Placement`,
  `@intrinsic pub fn shard(id: ShardId) -> Placement`. Opaque payload has no
  runtime meaning today.
- Sema records the destination expression in
  `CrossingLoweringInfo.Destination` (`internal/sema/crossing_lowering.go`);
  destination TYPE rules are Epic 11 sema work and do not change here.
- Runtime shard count and ids: `rt_runtime`/`rt_shard`
  (`runtime/native/rt_async_internal.h`), `RT_RUNTIME_MAX_SHARDS`, env
  `SURGE_SHARDS`.
- Precedent for intrinsic-to-runtime plumbing: how `blocking { ... }` and
  net intrinsics reach `runtime/native` through the LLVM backend
  (`internal/backend/llvm`) — map the exact path before designing.

## Design (implemented)

- Public Surge source signatures are unchanged:
  `@intrinsic @copy pub type Placement = { __opaque: int }`,
  `pool`, `distributed`, and `shard(id: ShardId)` remain the public surface.
- `Placement` lowers as a pointer-free `i64` tagged word in LLVM. Low 8 bits
  store the kind; upper bits store the payload:
  `pool = 1`, `distributed = 2`, `shard(id) = (id << 8) | 3`.
- Native resolver API is `rt_placement_resolve(rt_runtime*, uint64_t placement,
  uint32_t current_shard_id) -> rt_placement_resolution`.
- `shard(id)` resolves exactly when `id < runtime->shard_count`.
  Out-of-range returns `RT_PLACEMENT_STATUS_INVALID_SHARD`, destination
  `UINT32_MAX`, and increments `invalid_shard_resolutions`; it never clamps,
  modulo-maps, or falls back to shard 0.
- `distributed` uses a deterministic per-runtime round-robin cursor. For
  `shard_count > 1`, if the round-robin candidate is the caller shard, the
  resolver selects the next shard instead and increments
  `distributed_non_caller_resolutions`.
- `pool` and unknown placement kinds return `RT_PLACEMENT_STATUS_UNSUPPORTED`
  with no destination. Pool execution remains out of scope.
- Debug evidence stays resolver-local through
  `rt_placement_debug_snapshot`; `rt_async_trace.c` was not changed.

## Scope

In: `core/intrinsics.sg` payload comment updates (no signature changes),
LLVM backend lowering of the three placement intrinsics to the runtime
encoding, `runtime/native/rt_placement.c/.h` (or equivalent), resolver unit
rows, trace evidence.

Out: any crossing execution, any VM backend placement support (VM stays
diagnostic-only), any new placement kinds or syntax, migration.

## Steps

1. **Test-first:** native-level unit rows for the resolver (all kinds,
   boundary ids at `SURGE_SHARDS=1,2,8`, out-of-range rule, distributed
   policy distribution proof over N resolutions), plus a compile-level row
   that a program materializing `shard(3)` / `distributed` produces the
   expected encoded value (observable via a test hook, not via crossing
   execution).
2. Implement the runtime encoding + resolver.
3. Lower the intrinsics in the LLVM backend to produce the encoding.
4. Wire trace evidence for `distributed` selection.
5. Confirm `pool` and every unsupported kind resolve to the deterministic
   unsupported status, and that nothing in this task weakens the crossing
   guards (crossing forms still produce FUT7014-7017 on every backend —
   `make runtime-v2-crossing-check`).

## Proof

- `go test ./internal/backend/llvm -run '^TestPlacementIntrinsicsLowerAsTaggedI64$' -count=1 -v`
  passed. Proves `pool`, `distributed`, and `shard(3)` materialize as tagged
  `i64`, and no unresolved `@pool`, `@distributed`, or `@shard` call/global
  leaks into IR.
- `go test ./internal/sema -run 'TestUserDefinedPlacementIntrinsicDoesNotBecomeRuntimePlacement|TestSpawnOnCrossing|TestOnCrossing|TestCrossing' -count=1 --timeout 90s`
  passed. Proves user-defined `@intrinsic type Placement` lookalikes outside
  core are not marked as runtime Placement, while the core placement crossing
  matrix remains valid.
- `go test ./internal/backend/llvm -run 'TestPlacement|TestUserPlacement|TestUserShard' -count=1 -v --timeout 90s`
  passed. Proves user `Placement` aliases and user `shard` functions do not
  lower as runtime placement intrinsics.
- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Placement(ABIStaticShape|ResolverRows)$' -count=1 -parallel=1 -p=1 -v --timeout 60s`
  passed. Proves resolver rows for shard counts 1, 2, and 8, out-of-range
  fail-closed behavior, unsupported `pool`, unknown-kind unsupported status,
  and non-caller distributed evidence.
- `go test ./internal/diag ./internal/sema ./internal/driver -count=1 --timeout 90s`
  passed after `sema.Options` switched to an interned `ModulePath` and
  `diag.ReporterBag` kept alien-hints compatible with dedup reporters.
- `go test ./internal/mir ./internal/backend/llvm -count=1 --timeout 90s`
  passed.
- `make runtime-v2-crossing-check` passed.
- `make c-check` passed.
- `make cppcheck` scanned the new `runtime/native/rt_placement.c` without a
  new finding, but the target remains nonzero due to existing style findings:
  `runtime/native/rt_net.c:125` (`constVariablePointer`),
  `runtime/native/rt_net_handles.c:195` and `:352`
  (`constParameterPointer`).
- `./check_file_sizes.sh -a` passed. New placement files are OK; existing
  acceptable/legacy files remain within their configured ceilings.
- `sentrux check runtime/native` passed, quality 5455.
- `sentrux check internal` passed, quality 6533.
- Independent review found no remaining P0/P1 blockers after the provenance
  fixes. The review's P2 (`appendModuleInstantiations` missing `ModulePath`)
  was fixed before closeout.
- `make check` passed after the `sema.Options` size/lint fix.

## Stop Conditions

- The encoding cannot stay Copy/shard-movable without a pointer — stop;
  that breaks the Epic 11 type contract and needs design review.
- The out-of-range or distributed decision proves untestable as specified —
  return to Task 1's record, do not improvise a different rule in code.
