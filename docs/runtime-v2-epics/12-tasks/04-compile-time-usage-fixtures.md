# Epic 12 Task 4: Controlled Compile-Time Usage Fixtures

**Status:** pending.
**Kind:** fixtures + internal probes; no compiler logic changes expected.
**Depends on:** Task 2 (stable diagnostics), Task 3 (readiness record to
assert against).

## Goal

Prove the accepted Epic 11 surface survives the real pipeline in realistic
combinations — not just the per-feature fixtures Epic 11 landed — while
keeping everything clearly non-runnable. This is integration coverage for
the compile path, not examples.

## Starting State

- Epic 11 fixtures live under `testdata/golden/crossing/block0{1..4}/` and
  are feature-scoped (one construct per fixture family).
- `internal/crossinggate` drives them; backend-stage fixtures stay
  `_`-prefixed (`11-tasks/README.md`, "Backend-unavailable rows").
- `TcpConn`/`TcpListener` are `@shard_pinned @nosend` in
  `core/intrinsics.sg`; prelude declares `Placement`, `Task<T>`,
  `Channel<T>`.

## Scope

In: combined-surface fixtures, stdlib-facing compile-time probes, harness
glue for asserting Task 3 records on fixtures.

Out: anything under `examples/`; fixtures presented as runnable; new stdlib
public API; new syntax; runtime execution of any crossing form.

## Steps

### 1. Combined-surface fixtures

Add a `testdata/golden/crossing/integration/` family (naming per Task 1's
inventory) of compile-only positive fixtures that combine constructs the
Epic 11 matrix never combined, e.g.:

- `spawn on` whose body contains an `on` back to a placement, returning
  through both result types;
- a function that both awaits and cancels different `far Task<T>` values and
  is itself called directly (exercising inference chains);
- `@shard_movable` user types captured across `on` and `spawn on` in one
  module, alongside a rejected `@shard_pinned` capture negative;
- crossing sites inside generic functions instantiated more than once (the
  record from Task 3 must appear per site, not per instantiation collapse —
  or the observed behavior must be recorded and tested as-is);
- deeply nested control flow around `ret` inside crossing blocks.

Each positive fixture: compiles clean at sema stage, produces exactly the
expected FUT diagnostics (and nothing else) at backend stage. Each negative
keeps its `// EXPECT-DIAG:` header with an Epic 11 code — this task must not
invent new diagnostics; if a combination produces a wrong/ambiguous
diagnostic, stop and record it as a design-review finding per the epic's
"No new syntax without review" boundary.

### 2. Record assertions on integration fixtures

Extend the crossinggate harness (or a sibling test) so integration fixtures
also assert Task 3's readiness records: site count and per-site form kinds
for each fixture. This makes the integration family the regression net for
representation changes.

### 3. Stdlib-facing probes

Internal, compile-time-only probes (not public API, not examples) that apply
the surface to real stdlib types: e.g. a probe module proving
`far TcpConn` remote I/O is rejected (`SEM3151`), `Channel<T>` capture rules
behave per Block 4 across `spawn on`, and `@shard_pinned` on
`TcpConn`/`TcpListener` interacts correctly with captures. Location per Task
1's map (likely alongside existing sema/crossinggate testdata, not in
`stdlib/`); if any `stdlib/*.sg` annotation change turns out to be needed,
it must be compile-time-only and must not add public names for crossing
concepts.

### 4. Non-advertisement check

Verify no fixture or probe is reachable as a runnable example: nothing under
`examples/`, nothing referenced from README/docs as executable, and
backend-stage fixtures keep the `_` prefix. Record the check in closing
evidence.

## Proof Gates

- `go test ./internal/crossinggate/` (+ the sibling record-assertion tests)
- `make golden-update` + `make golden-check` for non-backend-stage fixtures
- `make check`
- `./check_file_sizes.sh -a`; root + `internal/` Sentrux scans

## Exit Criteria

- Integration fixture family exists, green at both stages, with record
  assertions.
- Stdlib-facing probes compile-time green; zero stdlib public-surface
  changes.
- Any construct that misbehaved is recorded as a design-review finding, not
  patched.
