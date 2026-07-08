# Epic 12 Task 2: Backend-Unavailable Diagnostic Contract

**Status:** pending.
**Kind:** compiler diagnostics + tests.
**Depends on:** Task 1 (guard-point map, ExprOn-reaches-HIR experiment).

## Goal

Turn the current ad-hoc guards into the contract the epic demands: stable
messages without internal epic numbers, default-closed backend gating, a
deterministic internal error on guard bypass, and a tested negative space
(compile-only paths never show the diagnostic on valid code).

## Starting State

- Guards: `internal/buildpipeline/on_crossing_check.go` (FUT7014) and
  `internal/buildpipeline/spawn_on_check.go` (FUT7015/7016/7017), called from
  `compile.go:91-92`.
- Both guards early-return for any backend other than `BackendVM` /
  `BackendLLVM` (`on_crossing_check.go:23`, `spawn_on_check.go:28`) —
  default-open.
- All four messages end with "the Phase 4 transport backend is unavailable" —
  contract violation (internal epic number).
- Diagnostic codes live in `internal/diag/codes_crossing.go` (FUT 7xxx
  range); codes are stable and stay, only messages change.
- Fixture proof: `internal/crossinggate/crossing_gate_test.go` and
  `spawn_on_backend_test.go` drive `_`-prefixed `// EXPECT-STAGE: backend`
  fixtures through `buildpipeline.Compile` with `BackendVM` and assert codes.
- Precedent for a VM-only guard: `blocking_check.go:16`
  (`FutBlockingNotSupported`, 7008) — do not change its behavior in this
  task.

## Scope

In: guard functions, their messages, their backend gating, an ICE check at
the HIR boundary, crossinggate fixtures for the new proofs, golden churn from
message changes.

Out: any change to sema acceptance rules, any new syntax, any HIR/MIR
representation work (Task 3), fixing artifact races (Task 5).

## Steps

### 1. Message contract

Rewrite the four messages to name (i) the crossing surface and (ii) the
boundary, without epic/phase numbers. Proposed shape (final wording decided
in-task, then frozen):

> `` `on` placement crossing cannot be executed: no available backend
> supports cross-shard transport ``

Record the frozen wording for all four codes in this document; from then on
message changes are breaking and need a documented reason. Codes
(FUT7014-7017) do not change.

### 2. Default-closed inversion

Replace the `!= BackendVM && != BackendLLVM → return` allowlist in both
guards with default-closed logic driven by Task 1's pipeline map:

- define a single predicate (e.g. `backendHasCrossingTransport(Backend)
  bool`) in one place; it returns `false` for every current value;
- guards skip only for entry paths that are genuinely non-executable
  (sema-only diagnose), as identified by Task 1 — and that skip must be
  keyed on "no backend selected", not on "backend not in list";
- an unknown/future `Backend` string value must hit the guard.

Add a unit test in `internal/buildpipeline` that compiles a crossing fixture
with a fabricated backend value and asserts the diagnostic still fires.

### 3. ICE on guard bypass

Based on Task 1's experiment: ensure an `ExprOn` node reaching HIR lowering
produces a deterministic internal-compiler-error diagnostic naming the
construct, never a silent skip or partial lowering. Implementation shape is
free (explicit `ast.ExprOn` case in `internal/hir/lower_expr.go` that reports
and aborts, or an assertion at lowering entry), but it must be reachable in a
test: add a test that invokes lowering directly (bypassing the pipeline
guard) on a module containing `on` and asserts the ICE, so the check cannot
rot.

### 4. Negative space

Add explicit tests that valid crossing code produces zero FUT7014-7017 on:

- `driver.Diagnose` (sema stage) — already implicitly true via compile-only
  positive fixtures; make it an explicit assertion;
- the LSP diagnostics path (per Task 1's map of where it builds diagnostics);
- format/fix drivers if Task 1 shows they run any pipeline stage that could
  reach the guards.

### 5. Per-form matrix rows

Ensure each of the five lowering-contract forms plus a direct call to an
inferred-crossing function has a backend-stage fixture for **both**
`BackendVM` and `BackendLLVM` (current fixtures cover VM only, per
`crossing_gate_test.go:58`). Reuse the `// EXPECT-STAGE: backend` mechanism;
extend the harness with a backend selector header (e.g.
`// EXPECT-BACKEND: llvm`) if needed, keeping `_`-prefix rules intact. If
LLVM-path compilation of these fixtures trips DEBT-011 artifact races, stop
and trigger the Task 5 promotion path instead of stabilizing by retries.

### 6. Golden churn

Regenerate goldens affected by message changes (`make golden-update`),
review the diff is message-text-only, commit sidecars, `make golden-check`.

## Proof Gates

- `go test ./internal/buildpipeline/ ./internal/crossinggate/`
- new unit tests from steps 2-4 named in the closing evidence
- `make golden-check`, `make check`
- `./check_file_sizes.sh -a` (guards live in small files; keep it that way)
- root + `internal/` Sentrux scans per `SENTRUX_POLICY.md`

## Exit Criteria

- Frozen message wording recorded here for FUT7014-7017; no "Phase"/"Epic"
  text in any of them.
- Fabricated-backend test proves default-closed behavior.
- ICE-on-bypass test exists and fails if the check is removed.
- Negative-space tests exist for sema/LSP paths.
- VM and LLVM rows exist for all five forms + direct crossing call.
