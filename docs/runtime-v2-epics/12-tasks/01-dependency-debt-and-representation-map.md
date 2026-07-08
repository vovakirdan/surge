# Epic 12 Task 1: Dependency, Debt, And Representation Map

**Status:** complete.
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

## Results (2026-07-08)

Task 1 is closed as a documentation/evidence task. No production code changed,
and the throwaway HIR-bypass worktree was removed after the spike.

### Guard-Point And Pipeline Map

| Entry path | Guard fires? | What reaches HIR? | Evidence |
| --- | --- | --- | --- |
| `buildpipeline.Compile` with `BackendVM` | Yes, today via allowlist. | Diagnostics are added before module diagnostics merge and before lowering stops on errors. | `internal/buildpipeline/compile.go:90-96`, `on_crossing_check.go:18-31`, `spawn_on_check.go:23-56`. |
| `buildpipeline.Compile` with `BackendLLVM` | Yes, today via allowlist. | Same as VM. | `internal/buildpipeline/compile.go:90-96`, `on_crossing_check.go:23`, `spawn_on_check.go:28`. |
| `buildpipeline.Compile` with any other non-empty backend | No today: both guards return before adding diagnostics; `Build` rejects unsupported backend only after `Compile`. Task 2 must invert this to default-closed. | If the unsupported backend path asks for HIR/MIR before the build rejection, `ExprOn` can reach lowering. | `internal/buildpipeline/on_crossing_check.go:23-24`, `spawn_on_check.go:28-29`, `build.go:63-75`. |
| `driver.Diagnose` / `surge diag` / shell golden sema rows | No backend guard by design. | HIR only runs when `DiagnoseOptions.EmitHIR` is true; otherwise parse/sema only. | `internal/driver/diagnose.go:327-333`; shell golden rows use sema diagnostics, not `buildpipeline.Compile`. |
| LSP diagnostics path | No backend guard by design. | Same diagnose-only path: valid crossing code must remain clean for editor diagnostics until a build backend is selected. | `internal/driver/diagnose/diagnose.go` delegates to `driver.DiagnoseWithOptions`; no `buildpipeline` call in the LSP path. |
| `surge fix` | No backend guard by design. | Diagnose-only path; fixes must not report backend-unavailable crossing diagnostics. | `cmd/surge/fix.go:106-107`. |
| `surge fmt` / formatter | No backend guard. | Parser + AST formatter only; no sema, no HIR, no backend. | `internal/driver/format.go:98-126`, `internal/format/doc.go:1-7`. |

### ExprOn-Reaches-HIR Experiment

The throwaway worktree `/tmp/surge-rv2-task1-hir-spike` locally disabled
`addOnCrossingBackendErrors` and `addSpawnOnBackendErrors`, then built a valid
`on pool { ret 1; }` program with both executable backends:

```bash
SURGE_STDLIB=/tmp/surge-rv2-task1-hir-spike \
  go run ./cmd/surge build --backend vm --ui off --keep-tmp --emit-mir /tmp/on_reaches_hir.sg

SURGE_STDLIB=/tmp/surge-rv2-task1-hir-spike \
  go run ./cmd/surge build --backend llvm --ui off --keep-tmp --emit-mir /tmp/on_reaches_hir.sg
```

Both failed with:

```text
MIR validation failed: function crossing_value: bb0: return without value in non-nothing function
```

Reason: `internal/hir/lower_expr.go:146-147` silently returns `nil` for
unhandled expression kinds; there is no `ast.ExprOn` case. Task 2 must add an
explicit ICE-on-bypass guard for `ExprOn` reaching HIR/lowering so a guard bug
cannot degrade into a vague MIR validation failure.

### Sema Metadata Inventory

| Contract row | Have / have-not | Current location and drop point |
| --- | --- | --- |
| `on` destination expression and destination type | Have enough at sema time; not persisted as a named lowering record. | `internal/sema/on_crossing.go:50-52` calls `checkOnDestination`; `checkOnDestination` types `data.Dest` at `:122` and derives far/placement shape at `:126-140`. The typed expression itself remains recoverable through AST + `Result.ExprTypes`, but no crossing-specific record survives. |
| `spawn on` destination expression and type | Have enough at sema time; not persisted as a named lowering record. | `internal/sema/spawn_on_crossing.go:43-45` and `:178-215` validate the destination; AST + `ExprTypes` retain the expression/type, but no crossing-specific record exists. |
| Captured payloads and movability judgments | Semantics are checked; the exact accepted capture set and judgment are dropped after diagnostics. | `typeExprOn` calls `checkOnCaptures` at `internal/sema/on_crossing.go:65-66`; `typeExprSpawnOn` calls it at `spawn_on_crossing.go:59-60`. Task 3 must persist any lowering-readable capture summary it needs. |
| `on` block result type | Have at sema time; expression type is persisted but payload/result pairing is not named. | `unifyOnBodyResults` at `internal/sema/on_crossing.go:155-185`; `TaskResult<T>` creation at `:76`; expression result is stored through normal `Result.ExprTypes`. |
| `spawn on` result type | Have at sema time; expression type is persisted as `far Task<T>`, payload is not named separately. | `internal/sema/spawn_on_crossing.go:62-73` computes payload and `farTaskType`; the expression type survives through `Result.ExprTypes`. |
| `far Task<T>.await()` / `.cancel()` spans and result types | Spans are persisted; result types are normal expression types. Receiver/payload metadata is not named as a lowering record. | `internal/sema/spawn_on_crossing.go:155-171` appends `FarTaskAwaitSpans` / `FarTaskCancelSpans` and returns `TaskResult<T>` / `TaskResult<nothing>`; `internal/sema/check.go:63-70` stores spans in `Result`. |
| Function crossing effect | Direct and direct-call transitive intra-module `MayCross` is persisted. Higher-order and cross-module propagation remain unresolved. | `internal/sema/crossing_effect.go:5-17` marks direct effects; `:41-67` finalizes call propagation; `internal/sema/check.go:62` stores `FunctionEffects`. |

### Debt Reconciliation

| Debt | Task 1 result | Owner / action |
| --- | --- | --- |
| `RV2-DEBT-001` | Still open. The broad focused command remains red for existing LLVM parity, terminal, and HTTP compatibility failures unrelated to crossing readiness. | Reassigned to the named future **Backend/Test Matrix Cleanup** epic. Epic 12 uses focused `buildpipeline` / `crossinggate` backend-unavailable rows instead of this broad command as a green gate. |
| `RV2-DEBT-002` | Still open. The MT liveness budget/isolation residue is not needed to prove compile-time crossing guards. | Reassigned to **Backend/Test Matrix Cleanup** with the existing compat-lane cleanup. |
| `RV2-DEBT-011` | Not promoted before Tasks 2/4. A focused overlap probe ran duplicate `TestLLVMBuildPortable` processes for 10 iterations (20/20 processes passed) and did not reproduce artifact races. | Remains open for Task 5 / later harness hardening. If Task 2/4 introduce VM artifact helpers, Task 5 is promoted then. |
| `RV2-DEBT-018` | Not reproduced by focused crossing-adjacent probes. | Remains open with `RV2-DEBT-011`; no early promotion. |
| `RV2-DEBT-024` | Not decided in Task 1. Current importer inventory shows no Task 2 consumer for imported crossing effects because guards are AST/sema-result based in the compiling module. | Task 3 owns the decision: implement/import effect metadata only if the chosen lowering-readiness record needs it. |

### Binding Representation Decision

Epic 12 uses option **(a) guard-before-HIR**.

Rationale:

- The HIR-bypass spike proves `ExprOn` currently becomes a malformed MIR path
  instead of a meaningful diagnostic when it reaches lowering.
- Sema already has enough source-level data to produce an explicit
  lowering-readiness record without introducing real HIR/MIR crossing nodes.
- Option (b) would pull affine `far T`, captures, move-plan, and borrow
  interactions into HIR/MIR before the Phase 4 transport design is ready.

Task 3 therefore records crossing lowering-readiness metadata derived from sema;
HIR/MIR remain crossing-free except for Task 2's ICE-on-bypass check.

### Test Command Inventory

Task 1 probes:

```bash
go test ./internal/buildpipeline ./internal/crossinggate -count=1
go test ./internal/crossinggate -run 'TestEpic11Block(2|3)|TestSpawnOnBackendGuards' -count=10
go test ./internal/sema -run 'Test.*Cross|Test.*SpawnOn|Test.*Far' -count=1
timeout 300s go test ./internal/vm -run 'MT|Async|Net|LLVM' -count=1 -v
timeout 1200s make runtime-v2-check
```

Results: the focused crossing/buildpipeline/sema commands passed. The broad VM
command failed for the known backend/test-matrix debt class and is not an Epic
12 green gate. The full `runtime-v2-check` baseline passed.

Later gates:

```bash
go test ./internal/buildpipeline -count=1
go test ./internal/crossinggate -count=1
make golden-check
make runtime-v2-crossing-check
./check_file_sizes.sh -a
make check
```

`make runtime-v2-crossing-check` is the candidate CI target name following the
existing `runtime-v2-*-check` pattern; Task 6 owns adding it after Tasks 2-5 are
stable.
