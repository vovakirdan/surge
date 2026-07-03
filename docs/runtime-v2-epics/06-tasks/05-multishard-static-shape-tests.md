# Task 5: Multishard Static Shape Tests

**Status:** Draft
**Kind:** test/static checks
**Depends on:** Task 1, Task 2, Task 3

## Context

Two static gates currently pin `N=1` at compile time:

- `internal/vm/runtime_v2_skeleton_static_test.go:22-26,34` —
  `#ifndef RT_RUNTIME_SHARD_COUNT` / `#error` guard plus
  `_Static_assert(RT_RUNTIME_SHARD_COUNT == 1, "runtime skeleton must be N=1")`.
- `internal/vm/runtime_v2_fd_registry_static_test.go:390-394` — the same
  `#ifndef`/`#error`/`#if RT_RUNTIME_SHARD_COUNT != 1` shape.

Both exist to make earlier epics' `N=1` claim mechanically checked, not just
asserted in prose (Global Rule 2: "which trace, test, or invariant exposes a
violation"). Task 6 will change `RT_RUNTIME_SHARD_COUNT` from a fixed literal
into a dynamic runtime `shard_count` bounded by a new
`RT_RUNTIME_MAX_SHARDS` (per the epic's stated preference). If Task 5 does
not update these gates first, Task 6 will break CI the moment it changes the
macro, discovering the break mid-implementation — exactly what the epic's
Starting State section calls out as the failure mode to avoid: *"Some
current Runtime V2 static gates deliberately pin `N=1`. Task 6 must update
those contracts instead of discovering the break mid-implementation."*

The epic also requires a **new** static gate this epic introduces: banning
shard-0-only net ownership shortcuts from reappearing, while explicitly not
failing legitimate global-compatibility paths (Epic 6 Boundary Decisions:
*"Static gates must forbid shard-0 fallback only for the net ownership path
that Epic 6 moves; they must not fail legitimate global compatibility
paths."*). This is the hardest part of this task, because the same
`rt_runtime_shard0()` function (`rt_runtime.c:50-55`) is currently called by
both net-owned accessors (`rt_executor_fd_registry`,
`rt_executor_net_poll_scratch`) and stays-global accessors
(`rt_executor_channel_blocking_compat`, `rt_executor_waiter_store` — which
also serves non-net waiter kinds). A test that bans "any call to
`rt_runtime_shard0`" would break legitimate global-compat callers; a test
that bans nothing would let Task 6-11 quietly regress to shard-0-only net
behavior. Task 2's dependency map is the required input for drawing this
line correctly — do not attempt this test without it.

## Goal

Update the existing `N=1` static pins to the new dynamic-shard contract, and
add a static/structural check that forbids net-ownership shard-0 shortcuts
without breaking legitimate global-compatibility call sites.

## Why This Task Exists

Static gates are cheaper to run than behavior tests and catch structural
regressions (a new call site silently reintroducing `shard0()` for net
ownership) that a behavior test might not exercise if the test happens to
only use one shard's connections. `RULES.md` Global Rule 4 also requires
every task touching an over-limit file to record line-count impact; adding a
static gate for `rt_runtime.c`/`rt_net.c` symbols is one of the few checks
that can be enforced without running the full runtime.

## Scope

- Rewrite the `N=1` `_Static_assert`/`#error` pins in
  `runtime_v2_skeleton_static_test.go` and
  `runtime_v2_fd_registry_static_test.go` to assert the new contract instead
  (e.g. `RT_RUNTIME_MAX_SHARDS` is defined and `>= 1`, `rt_runtime.shard_count`
  is a runtime value bounded by it, not a compile-time-fixed `1`). Do not
  delete the historical intent of these tests — they still exist to prove
  the runtime shape is well-formed, just against the new dynamic contract.
- Add a static/structural test (Go-level source scan, `nm`/symbol check, or
  a dedicated `_Static_assert` shape, whichever fits the existing pattern
  best) that fails if a net-ownership entry point (fd registry attach/detach/
  mark-closed/complete, net poll scratch, accept distribution) resolves
  through `rt_runtime_shard0()` without an explicit shard index/argument,
  while allowing the already-classified stays-global-compat call sites from
  Task 2's map to keep using it.
- Add a static check for the `RT_RUNTIME_MAX_SHARDS` vs. `rt_shard
  shards[...]` array-size relationship Task 6 is expected to introduce
  (e.g. assert the array is sized to the max, not to a fixed `1`, once Task
  6 lands — this test can be written now against the *intended* macro name
  and marked `runtime_v2_pending` until Task 6 defines it).
- Document, in the test file or in `06-evidence.md`, exactly which existing
  call sites are intentionally exempt from the new net-ownership gate (the
  "stays-global-compat" list from Task 2) so a future reviewer does not
  mistake an exemption for a missed case.

## Out Of Scope

- Behavior tests (Task 4's job).
- Actually implementing `RT_RUNTIME_MAX_SHARDS` or changing
  `rt_async_internal.h` (Task 6's job) — this task writes tests against the
  intended shape, gated `runtime_v2_pending` where the shape does not exist
  yet.
- The no-steal scheduler static proof (that is part of Task 7's own
  behavior/invariant work, though this task may add a compile-time shape
  check for owner-shard task metadata if Task 2's map already names exactly
  where that field will live).

## Approach / Steps

1. Read Task 2's dependency map for the exact list of net-shard-owned vs.
   stays-global-compat call sites.
2. Update the two existing static pin test files to assert the new
   dynamic-shard-count contract; keep them buildable and green against
   today's still-`N=1`-only code (they should assert "shape is well-formed
   for dynamic N", which is trivially true when N happens to still be 1).
3. Design the net-ownership-shortcut gate. Prefer a mechanical check over a
   hand-maintained allowlist if one exists (e.g., grep-based Go test that
   parses `rt_runtime.c` and fails if a *new* function calling
   `rt_runtime_shard0()` appears in the net-owned symbol list beyond the
   ones Task 2 already named); if a mechanical check is impractical, use an
   explicit allowlist test and record why a mechanical check was rejected
   (Global Rule 5: "why the existing primitive is wrong or insufficient").
4. Write the `runtime_v2_pending`-gated shape test for
   `RT_RUNTIME_MAX_SHARDS` against the name Task 2/3 settled on.
5. Run both updated files plus the new gate against current (`N=1`) code;
   confirm the historical pins still pass in their updated form and the new
   gate does not false-positive on any existing legitimate call site.
6. Update `06-evidence.md` and `NOTES.md`.

## Files

Read:

- `internal/vm/runtime_v2_skeleton_static_test.go`
- `internal/vm/runtime_v2_fd_registry_static_test.go`
- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2)
- `runtime/native/rt_runtime.c`, `rt_async_internal.h`, `rt_net.c`

Update:

- `internal/vm/runtime_v2_skeleton_static_test.go`
- `internal/vm/runtime_v2_fd_registry_static_test.go`

Create:

- A new static test file for the net-ownership-shortcut gate, e.g.
  `internal/vm/runtime_v2_accept_static_test.go` (name it to match sibling
  conventions once Task 3/2 settle terminology)
- Update `docs/runtime-v2-epics/06-evidence.md`, `NOTES.md`

## Skills & Working Practice

- Global Rule 9 plan gate applies: state the exact mechanism chosen for the
  net-ownership gate (mechanical scan vs. allowlist) before writing it, since
  this is the one part of this task with real design judgment.
- May be planned in parallel with Task 4 (disjoint files), but both must
  agree on terminology with Task 3's chosen listener model and Task 2's
  classification before either is finalized — do not let the two diverge on
  what counts as "net-owned."
- Keep new test files at or below the Global Rule 4 500-line limit; split
  before growing past it.

## Checks

- `go build ./...` (static/compile-time assertions must not break the
  build)
- `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Skeleton|TestRuntimeV2FDRegistryStatic'`
- `go test -tags runtime_v2_pending ./internal/vm -run '<new net-ownership gate test name>'`
  (expected fail against current code until net-owned accessors stop routing
  through shard 0)
- `go test -tags runtime_v2_pending ./internal/vm -run '<pending MAX_SHARDS shape test>'`
  (expected fail until Task 6, in the "not defined yet" shape)
- `git diff --check`

## Definition Of Done

- [ ] Both existing `N=1` static pins are rewritten to assert the dynamic
      shard-count contract and still pass against current code.
- [ ] A new static gate exists that fails on a net-ownership call routed
      through `rt_runtime_shard0()` without shard-index awareness, while not
      failing any documented stays-global-compat call site.
- [ ] The exemption list (stays-global-compat call sites) is written down
      next to the gate, not left implicit.
- [ ] A `runtime_v2_pending` shape test exists for `RT_RUNTIME_MAX_SHARDS`
      against the name Task 6 will use.
- [ ] Task 6 can change `RT_RUNTIME_SHARD_COUNT` without being surprised by
      either static pin.

## Evidence To Record

- `06-evidence.md`: Contracts Touched (map each static gate to the contract
  bullet it protects), Commands/Checks (exact `go test`/`go build` results).
- `NOTES.md`: the exemption list and the reasoning for mechanical-scan vs.
  allowlist, so Task 6/7/9/11 know which gate they must keep satisfying.
