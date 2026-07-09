# Epic 13 Task 5: Placement ABI And Destination Resolution

**Status:** pending.
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

## Design (from Task 1 decisions; restate here when made)

- `Placement` runtime encoding: a tagged word (kind: POOL / DISTRIBUTED /
  SHARD + shard id payload) is the expected shape; must stay Copy and
  shard-movable, and must not leak pointers.
- `shard(id)` out-of-range rule: exactly the Task 1 decision (clamp / error /
  modulo), implemented once in the runtime resolver and tested.
- `distributed` policy: exactly the Task 1 decision (round-robin / hash /
  non-current-shard), implemented in the resolver with a trace counter or
  trace event proving a non-caller destination is selectable.
- Resolution API (suggested): `rt_placement_resolve(placement, current_shard)
  -> shard_id` with status for unsupported kinds; `pool` resolves to a
  status the caller must turn into the placement-unavailable diagnostic path
  (or is rejected before runtime — record which layer owns it).

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

- Resolver unit rows green at `SURGE_SHARDS=1,2,8`.
- Distribution proof: over N `distributed` resolutions with >1 shards, at
  least one non-caller destination (per the chosen policy's guarantee).
- `make runtime-v2-crossing-check` green (guards untouched by ABI work).
- `make c-check`, `make cppcheck`, `./check_file_sizes.sh -a`,
  `sentrux check runtime/native` and `internal` if backend files changed,
  `make check`.

## Stop Conditions

- The encoding cannot stay Copy/shard-movable without a pointer — stop;
  that breaks the Epic 11 type contract and needs design review.
- The out-of-range or distributed decision proves untestable as specified —
  return to Task 1's record, do not improvise a different rule in code.
