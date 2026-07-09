# Epic 13 Task 7: Compiler Lowering Representation

**Status:** complete.
**Kind:** compiler HIR/MIR + guard split. Backend-neutral representation;
no form becomes executable in this task.
**Depends on:** Task 1 (map). May run parallel to Tasks 3-6.

## Goal

Introduce typed HIR/MIR representation for the supported crossing forms and
split the backend guards by transport capability — while every form still
fails deterministically on every backend, because no vertical is enabled
until Tasks 8-10. This task converts Epic 12's guard-before-HIR readiness
shape into the transitional shape the epic's Lowering Contract requires.

## Starting State (verify and re-pin)

- Guards: `internal/buildpipeline/on_crossing_check.go`,
  `spawn_on_check.go`, predicate `crossing_transport.go:14-16`
  (`backendHasCrossingTransport` — constant `false` today); guards scan root
  + dependency modules (`DependencyAnalyses`, commit `2fce7c22`).
- HIR: `internal/hir/lower_expr.go:120-126` — `ast.ExprOn` is a
  deterministic ICE; `internal/hir` / `internal/mir` have NO crossing nodes.
- Sema metadata: `internal/sema/crossing_lowering.go` records per-site
  `CrossingLoweringInfo` (destination, captures, payload/result/handle
  types, receiver consumption, remote ops) — the lowering input contract
  from Epic 12's closeout.
- Pipeline order caveat (Task 1 map): `driver.DiagnoseWithOptions` runs HIR
  lowering before the buildpipeline guards and discards HIR errors
  (`internal/driver/diagnose.go`, `//nolint:errcheck`). Introducing real HIR
  crossing nodes changes what that ordering means — this task must make the
  order explicit: guards decide BEFORE crossing nodes lower, or lowering is
  capability-gated; silent reliance on discard is not acceptable.
- Tests that pin today's behavior and will churn here:
  `TestLowerOnCrossingBypassReturnsError`,
  `TestLowerSpawnOnCrossingBypassReturnsError` (`internal/hir/lower_test.go`),
  `TestCrossingBackendGuardsAreDefaultClosed`,
  `TestCrossingBackendGuardsCoverImportedModules`
  (`internal/buildpipeline/crossing_backend_test.go`).

## Design Constraints

- Representation is backend-neutral (the "optional VM transport later"
  handoff must not be foreclosed).
- Guard split: `backendHasCrossingTransport(backend)` becomes per-form
  capability (e.g. `backendSupportsCrossingForm(backend, form)`), still
  default-closed: unknown backends and unsupported forms keep FUT7014-7017
  (or the more precise placement-unavailable diagnostic for `pool`). In THIS
  task every (backend, form) pair still answers "unsupported".
- The HIR ICE backstop remains for impossible bypasses: a crossing node that
  reaches lowering on a backend that does not support it is still a
  deterministic internal error.
- Typed nodes/instructions must carry everything the runtime APIs need:
  destination value, capture list with representations (per the Task 1
  payload decision), result type, handle type + consumption for far-task
  ops, and the execute/reply distinction for immediate `on` (dedicated
  category — NOT a spawn+await desugar).
- Async lowering integration: `on` / `spawn on` / await / cancel are suspend
  points; the state-machine lowering (`mir.LowerAsyncStateMachine`) must
  treat the transport reply wait as a resume kind. Map where before coding.

## Scope

In: HIR node kinds + lowering from AST/sema records, MIR
instructions/resume kinds, guard-split plumbing, capability predicate, test
churn for the pinned tests above, new representation tests.

Out: enabling any (backend, form) pair (Tasks 8-10 flip them), LLVM codegen
to the runtime APIs (Tasks 8-10), VM support, sema acceptance changes.

## Steps

1. **Test-first:** representation tests that lower each supported form to
   HIR/MIR under a test-only capability override and assert the typed
   payload (destination, captures, types, consumption) survives — the
   Epic 12 "per row a test that fails if information is lost" rule, now at
   the IR layer. Plus guard tests: with the override OFF (production state),
   every form on every backend still produces its FUT diagnostic and no
   crossing node reaches codegen.
2. Introduce HIR crossing nodes; replace the blanket `ExprOn` ICE with:
   lower when capability-enabled, ICE otherwise (message unchanged).
3. Introduce MIR instructions/resume kinds; extend `mir.Validate` to reject
   crossing instructions on non-capable backends (deterministic error, not
   silent drop).
4. Split the guard predicate per form; keep default-closed semantics and the
   dependency-module scan; update the pinned tests deliberately (their
   churn is expected and reviewed, not accidental).
5. Prove compile-only negative space is intact
   (`internal/crossinggate/negative_space_test.go` still green — LSP/check/
   format/fix see no FUT diagnostics on valid code).

## Proof

- Backend guard split is default-closed for VM, LLVM, and unknown backends
  across `on` placement, `on` far-handle, `spawn on`, far `Task.await`, and
  far `Task.cancel`; imported-module crossing records are guarded before HIR
  lowering.
- HIR lowers all five crossing forms only through explicit test capability
  flags and keeps deterministic ICE backstops when the production capability
  set is empty.
- Mono clone/subst/traversal/type collection preserves the new HIR crossing
  node; MIR lowers all five forms only through explicit test capability flags,
  validates crossing instructions as default-closed, and models async crossing
  suspension with ready/pending blocks.
- Negative-space diagnostics remain out of plain driver, workspace/LSP,
  format, and fix paths for valid `on`, `spawn on`, far `Task.await`, and far
  `Task.cancel` code.
- Proof commands run in this pass:
  - `go test ./internal/buildpipeline ./internal/hir ./internal/mono ./internal/mir -run 'Crossing|LowerOnCrossingBypass|LowerSpawnOnCrossingBypass|LowerFarTaskCrossingBypass|MonoPreservesCrossingRepresentation|MIRCrossing|MIRAsyncCrossing' -count=1 --timeout 90s`
  - `go test ./internal/crossinggate -run 'TestBackendUnavailableNegativeSpace' -count=1 -v --timeout 60s`
  - `make runtime-v2-crossing-check` (twice)
  - `git diff --check`
  - `./check_file_sizes.sh -a`

## Stop Conditions

- A supported form cannot be represented without changing sema acceptance or
  Epic 11 semantics — stop, record the construct, design review.
- The async state machine needs a transport resume kind that contradicts the
  Task 4/6 wait mechanism — stop and reconcile with the runtime authors
  before landing either side.
