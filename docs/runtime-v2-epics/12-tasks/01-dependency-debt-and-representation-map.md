# Epic 12 Task 1: Dependency, Debt, And Representation Map

**Status:** pending.
**Kind:** map/evidence + two binding decisions. No production code changes;
read-only probes and throwaway experiments are allowed but must not be
committed.
**Depends on:** none.

## Goal

Produce the map every later task executes against, and record the two
decisions that shape the epic: the representation decision and the debt
disposition. The map must be re-derivable from the repository alone.

## Starting State (verify, then anchor with current lines)

The anchors below were true when this document was written; the task must
re-verify each one and update line numbers in its output map.

Guard call sites and shape:

- `internal/buildpipeline/compile.go:91-92` calls
  `addOnCrossingBackendErrors` and `addSpawnOnBackendErrors` during `Compile`.
- `internal/buildpipeline/on_crossing_check.go:23` and
  `internal/buildpipeline/spawn_on_check.go:28` both early-return unless
  `req.Backend` is exactly `BackendVM` or `BackendLLVM` — the default-open
  allowlist the epic requires Task 2 to invert.
- `internal/buildpipeline/types.go:52-58`: `Backend` is a string type with
  exactly two values, `"vm"` and `"llvm"`.
- Guard messages hardcode "the Phase 4 transport backend is unavailable"
  (`on_crossing_check.go:10`, `spawn_on_check.go:11-13`), violating the
  epic's diagnostic contract (no internal epic numbers).
- The guards scan the AST expression arena for `ExprOn` nodes
  (`collectOnCrossingSpans`, `collectSpawnOnSpans`) and read
  `diagRes.Sema.FarTaskAwaitSpans` / `FarTaskCancelSpans` for the task
  operations.

Representation gap:

- `internal/hir` and `internal/mir` contain no `ExprOn` handling at all
  (verified by grep at draft time). A crossing program only avoids HIR
  because the guard adds errors first and compilation stops on errors.
- Sema records inferred crossing effects in
  `internal/sema/crossing_effect.go` (`FunctionEffects` map,
  `finalizeFunctionEffects` at `:41`, `MayCross` propagation over direct
  calls at `:56-61`).

Fixture harness:

- `internal/crossinggate/crossinggate.go`: all four block gates are `true`.
- Backend-unavailable fixtures use `// EXPECT-STAGE: backend` and stay
  `_`-prefixed, out of the shell golden corpus
  (`11-tasks/README.md`, "Backend-unavailable rows").

## Deliverables

### 1. Guard-point and pipeline map

For every compiler entry path, record whether the crossing guards run and
what happens to an `ExprOn` node on that path:

- `buildpipeline.Compile` with `BackendVM` / `BackendLLVM` / any other value
  (including what `build.go:72` does with unknown backends);
- `driver.Diagnose` (sema-only path used by `surge diag`, golden fixtures);
- the LSP diagnostics path (`internal/lsp`);
- `fix` / `format` drivers if they build ASTs.

Deliverable: a table "entry path → guard fires? → what reaches HIR?" with
`file:line` per row. This is the input for Task 2's default-closed inversion
and negative-space tests.

### 2. ExprOn-reaches-HIR experiment

Determine empirically what happens today if an `ExprOn` node reaches HIR
lowering (e.g. by locally disabling the guard in a throwaway build): a panic,
a silent skip in a `default:` branch, or a partial lowering. Record the
observed behavior with the exact code path
(`internal/hir/lower_expr.go` default branches at `:146`, `:173`). Do not
commit the experiment. This decides whether Task 2's ICE-on-bypass is a new
check or a hardening of an existing failure.

### 3. Sema metadata inventory

For each row of the epic's lowering-contract table, record where sema already
holds the required information and where it is dropped:

- destination expression and its type (`internal/sema/on_crossing.go`,
  `internal/sema/spawn_on_crossing.go`);
- captured payloads and their movability judgments (the `SEM3165`-`SEM3168`
  capture checks);
- block result types (`TaskResult<T>`, `far Task<T>`);
- `FarTaskAwaitSpans` / `FarTaskCancelSpans` and their result types;
- `FunctionEffects` / `MayCross` and where a backend-layer consumer could
  read it.

Deliverable: per-row "have / have-not / dropped-at" table. This is Task 3's
work list.

### 4. Debt reconciliation

For `RV2-DEBT-001`, `-002`, `-011`, `-018`: run the referenced commands,
record current failure modes, and decide per debt: **close here** (name the
task), **narrow** (state exactly which part this epic needs), or **reassign**
(write the new owner into `DEBT.md` — a named future epic, not a
placeholder). Expected per the epic: 011/018 in-scope via Task 5; 001/002
narrowed to what the crossing matrix needs. If 011/018 currently make
repeated `internal/crossinggate` or VM/LLVM matrix runs flaky, promote Task 5
ahead of Tasks 2 and 4 and update the index README.

`RV2-DEBT-024` is not decided here; Task 3 owns it. This map only records
where cross-module calls into crossing functions would surface (importer
inventory).

### 5. Representation decision (binding)

Choose and record with rationale:

- **(a) guard-before-HIR**: guards stay at the pipeline boundary; Task 3
  delivers an explicit, tested lowering-readiness record derived from sema
  metadata; HIR/MIR stay crossing-free except for the ICE-on-bypass check.
- **(b) lower-into-HIR-then-guard**: real HIR/MIR nodes for `on` /
  `spawn on`; guard moves into the backend layer. Requires a new task
  document (affine `far T` in HIR, capture/moveplan/borrow interactions)
  inserted before Task 3 and an index update.

The decision criteria: what Phase 4 actually needs at handoff (see the epic's
Handoff section), the cost of option (b)'s borrow/moveplan work, and whether
option (a) can still produce per-row tests that fail when information is
lost. Default expectation at draft time is (a); overturning it requires
written evidence that (a) cannot satisfy the lowering-contract table.

### 6. Test command inventory

Exact commands for later gates: the `crossinggate` run, focused
`buildpipeline` tests, the golden pipeline (`make golden-check`,
`scripts/golden_update.sh` `_`-prefix rule), and the candidate CI gate names
(follow the `runtime-v2-*-check` Makefile pattern).

## Exit Criteria

- All six deliverables recorded in this document (or a sibling evidence doc)
  with verified `file:line` anchors.
- Representation decision and debt dispositions written; index README updated
  if Task 5 is promoted or a Task 2.5/3-pre document is inserted.
- No production code changed; no experiment committed.
