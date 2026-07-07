# Epic 10: Runtime Debt Burn-Down And Owner-Safety Hardening

**Goal:** close the Runtime V2 debts that can make Phase 4 crossing work unsafe
or unnecessarily noisy: dependency-aware runtime cleanup (`RV2-DEBT-003`),
copied/stale net-handle safety (`RV2-DEBT-010`), and stdlib HTTP owner-shard
safety (`RV2-DEBT-013`). Epic 9 closed the local wakeup/cancel/accept-transition
ordering gaps. Epic 10 now removes the remaining owner-safety ambiguity and
reduces the code/coupling debt we would otherwise carry into explicit crossing
syntax and transport work.

This is a stabilization epic, not a feature-capacity epic. It should make the
current Runtime V2 easier to reason about, safer under `SURGE_SHARDS>1`, and
less dependent on legacy file-size exceptions. It is allowed to say "do not
ship this public multi-shard path yet" if that is the honest current contract,
but it must encode that contract as tests, runtime guards, or docs rather than
leaving it implicit.

**Status:** complete. Task documents live under `10-tasks/`; Epic 10 closed
`RV2-DEBT-003`, `RV2-DEBT-010`, and `RV2-DEBT-013` without syntax changes or
Phase 4 transport.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/09-wakeup-and-cancellation-safety.md`
- `docs/runtime-v2-epics/09-tasks/05-epic-closeout.md`
- `docs/runtime-v2-epics/09-evidence.md`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_task_park.c`
- `runtime/native/rt_waiter*.c`
- `runtime/native/rt_net*.c`
- `runtime/native/rt_channel*.c`
- `stdlib/http/server.sg`
- `stdlib/net*.sg` and any native net wrapper/lowering files used by `TcpConn`
  and `TcpListener`
- `internal/vm/runtime_v2_*_test.go`
- `scripts/bench_native_channels.sh`
- `scripts/bench_native_net.sh`
- `Makefile`

## Starting State After Epic 9

Epic 9 closed the three local safety debts that directly affected wakeup,
cancellation, and owner replacement:

- `RV2-DEBT-023`: cancel racing `RUNNING -> WAITING` park now uses an
  unconditional owner-shard wake token.
- `RV2-DEBT-020`: owner replacement now publishes and revalidates an atomic
  join-owner route before migrating join waiters.
- `RV2-DEBT-022`: external await and completion now use one seq-cst StoreLoad
  handshake; the guarded `done_cv` broadcast helper lives in `rt_done_cv.c`.

The remaining blockers before Phase 4 are different in kind:

- `RV2-DEBT-003`: at the start of Epic 10, `rt_async_state.c` remained over
  the Runtime V2 line target,
  and Sentrux gate still reports the cumulative coupling/complexity recovery
  class. The remaining split candidates are ready-queue, completion/cancel, and
  handle lifetime.
- `RV2-DEBT-010`: at the start of Epic 10, public copied net handles still
  exposed raw native fd state.
  Registry generations protect poll snapshots and waiter completion, but a
  stale copied handle can still conceptually act on a reused OS fd unless a
  stable handle/generation contract is added or public behavior is narrowed.
- `RV2-DEBT-013`: at the start of Epic 10, `stdlib/http/server.sg` sent raw
  `TcpConn.__opaque` handles through a channel to worker tasks. Under
  `SURGE_SHARDS>1`, those workers could run off the accepted connection's owner
  shard, so read/write ownership was not guaranteed by the public surface.

## Closeout State

Epic 10 landed the following runtime-only owner-safety changes:

- `RV2-DEBT-003` closed by splitting `rt_async_state.c` into dependency-owned
  ready-queue, completion/cancel, and task-lifetime modules while keeping the
  normal LOC gate green.
- `RV2-DEBT-010` closed by replacing public TCP handle words with stable
  runtime handle ids. Native net entrypoints canonicalize copied handles
  through the handle table before reading fd/owner/closed/generation fields;
  stale handles are removed on close and fail with `NET_ERR_NOT_CONNECTED`.
- `RV2-DEBT-013` closed by removing the `stdlib/http` raw `TcpConn.__opaque`
  worker handoff. `http.serve` now uses fixed local accept workers; accepted
  connections are handled on the owner-local task path, and
  `runtime-v2-http-owner-check` covers `SURGE_SHARDS=1,2,8`.

The epic did not add language syntax, `far`/`submit_to`/`crosses`, inbound
queues, remote `select`, eventfd credit accounting, remote-free queues, or
alternate I/O backends. Runtime V2 still treats those as later Phase 4+ work.

## Boundary Decisions

**No language syntax.** Epic 10 must not change Surge keywords, parser rules,
semantic checks for new syntax, lowering for explicit crossing, public examples
for crossing, or the names `far`, `submit_to`, `crosses`, or `shard-movable`.
Any task that appears to need those concepts stops for a dedicated language
syntax review with the user.

**No Phase 4 transport.** Do not implement inbound queues, remote messages,
eventfd credits, remote `select`, cross-shard cancellation messages, remote-free
queues, or the seq-cst Phase 4 `PARKED` protocol. Epic 10 may add local guards
or stable ids that later transport can reuse, but it must not smuggle in a
cross-shard messaging model.

**Owner safety before public capability.** If copied net handles or stdlib HTTP
cannot be made safely multi-shard without language/runtime crossing, the correct
Epic 10 result is an explicit guard and documented compatibility boundary, not
an implicit best-effort path.

**Refactor follows dependency ownership.** `RV2-DEBT-003` work must reduce real
dependency complexity. Moving lines out of `rt_async_state.c` is not enough.
Each split must explain the state it owns, the locks/atomics it depends on, the
paths that call it, and the Sentrux coupling/complexity effect. A split that
makes wakeup/cancel/cleanup harder to reason about fails the epic.

**Do not burn unrelated debt blindly.** Epic 10 owns only the debts listed below.
Other ledger items may be touched only if they block the proof for an owned
surface or the task document explicitly asks for that narrow scope.

## Debt Ownership

Epic 10 owns:

- `RV2-DEBT-003`: dependency-aware cleanup of `rt_async_state.c`, with the
  Sentrux coupling dimension re-checked. Close only if the file is split by real
  responsibility, removed from `.loc-legacy-allowlist`, and the remaining
  coupling is reduced or explicitly justified with evidence.
- `RV2-DEBT-010`: copied/stale net-handle safety. Close only if public net
  handle operations validate a stable handle id or registry generation before
  fd operations, and stale copied handles cannot act on a reused OS fd.
- `RV2-DEBT-013`: stdlib HTTP/server owner-shard safety. Close only if
  non-owner `TcpConn`/`TcpListener` use is rejected or proven owner-local under
  `SURGE_SHARDS>1`, and the HTTP server path is either owner-safe or explicitly
  single-shard compatibility with tests/guards.

Epic 10 may touch:

- `RV2-DEBT-006`: only if benchmark tooling blocks owner-safety/perf proof.
- `RV2-DEBT-011` / `RV2-DEBT-018`: only if focused owner-safety tests need
  artifact isolation to be trustworthy.
- `RV2-DEBT-007`: only if Sentrux threshold calibration is needed after the
  `RV2-DEBT-003` cleanup actually lands.

Epic 10 does not own:

- `RV2-DEBT-001`, `RV2-DEBT-002`: broad VM/native/LLVM matrix and MT budget
  rewrite remain Epic 12.
- `RV2-DEBT-005`: unrelated large native files remain later cleanup unless a
  task's real owner surface touches them.
- `RV2-DEBT-012`: heap benchmark high-pressure crash remains heap/test debt.
- `RV2-DEBT-017`: sync-channel compatibility latency remains with the future
  sync-compat retirement work.

## Executed Work Shape

Epic 10 executed as five task documents:

1. **Dependency and debt map.** Re-read current code and wrote the exact map for
   `rt_async_state.c` split candidates, copied net handles, stdlib HTTP handle
   flow, and relevant tests/gates.
2. **`RV2-DEBT-003` cleanup tranche.** Split only clusters with clean dependency
   boundaries and recorded before/after LOC plus Sentrux evidence.
3. **Copied net-handle contract.** Landed stable runtime handle ids for public
   net handles and canonical lookup before native fd operations.
4. **Stdlib HTTP owner-safety.** Removed the HTTP raw `TcpConn.__opaque`
   worker-pool handoff and added owner-local multi-shard behavior coverage.
5. **Closeout.** Reconciled `DEBT.md`, `NOTES.md`, `README.md`,
   `docs/RUNTIME_V2.md`, stdlib docs, Sentrux results, gates, and next-epic
   handoff.

## Proof And Quality Contract

Every implementation slice must start with a written model:

- the owned state and lifetime being changed;
- the public or internal contract before the task;
- the old unsafe or ambiguous path;
- the new invariant;
- the test, static gate, trace, or benchmark that would fail if the invariant
  regressed.

Required proof coverage for `RV2-DEBT-003`:

- before/after dependency map for each extracted cluster;
- static or behavior gate proving moved functions preserve their owner-lane,
  control-lane, and atomic contracts;
- `rt_async_state.c` effective LOC decreases and its allowlist ceiling is
  lowered or removed;
- Sentrux root, `runtime/`, and `runtime/native` checks recorded, plus gate
  verdicts and coupling/complexity explanation.

Required proof coverage for `RV2-DEBT-010`:

- stale copied handle cannot operate on a reused fd;
- copied handle behavior is defined for close, read, write, clone/copy, and
  post-close use;
- non-owner operation under `SURGE_SHARDS>1` is either rejected with a stable
  error path or proven owner-local;
- fd registry generation/id checks are tested with positive and negative cases.

Required proof coverage for `RV2-DEBT-013`:

- current `stdlib/http/server.sg` handler placement is traced under
  `SURGE_SHARDS=1`, `2`, and `8`;
- HTTP handler `TcpConn` operations are owner-local, or the multi-shard path is
  explicitly denied/falls back with a test proving the denial/fallback;
- no raw `TcpConn.__opaque` transfer path remains unaccounted for;
- if a single-shard compatibility boundary remains, it is named in docs and
  enforced by tests or runtime guard.

Every runtime-code task must run and record:

- `git diff --check`;
- `make c-check`;
- `make cppcheck` for native C changes;
- focused Go tests named in the task document;
- `make runtime-v2-check`;
- `make check`, unless the task records a narrower approved gate and the final
  closeout runs the full gate;
- `./check_file_sizes.sh -a` when C/H/Go files change;
- root, `runtime/`, and `runtime/native` Sentrux scans per
  `SENTRUX_POLICY.md`;
- effective LOC for every touched over-limit file and every new runtime file.

Benchmarks are required when a task changes request-path net handle checks,
HTTP serve flow, scheduler placement, or task lifecycle hot paths. Use trace
counters to explain any overhead; wall-clock throughput alone is not a proof.

## Performance Contract

Epic 10 is not expected to improve throughput. It must preserve the Epic 8/9
control-lane and placement results unless a task explicitly records a
temporary, justified regression with an owner and recovery plan.

Closeout must record final `TestRuntimeV2PerfControlLaneGate` counters and
explain any material increase in:

- `control_lock_acquired`;
- `ctrl_await_compat`;
- steady-state-control/request;
- lifecycle-control/request;
- `placement_adoptions`;
- `accept_owner_active_shards`.

Any new handle-generation check on the TCP hot path must be measured. If the
cost is non-zero, the task must explain why it is acceptable or move the check
to a colder boundary.

## Subagent Plan

Use subagents for independent slices after task documents exist. Each
implementation subagent must first return a plan and wait for approval. A
separate review/testing subagent must inspect the same scope before the task
closes. Do not run two implementation subagents against the same C files,
stdlib files, or test harness files unless the write sets are explicitly split.

Likely independent review surfaces:

- `RV2-DEBT-003` dependency map and Sentrux/LOC evidence;
- stale copied net-handle safety and registry generation/id checks;
- stdlib HTTP handler placement and owner-shard proof;
- benchmark/test harness reliability if `RV2-DEBT-006`/`011`/`018` are touched.

## Epic Acceptance

Epic 10 is complete only when:

- `RV2-DEBT-003` is either closed by dependency-aware cleanup or explicitly
  narrowed with a smaller remaining owner and fresh Sentrux/LOC evidence;
- `RV2-DEBT-010` is closed or narrowed to a clearly enforced compatibility
  boundary with tests;
- `RV2-DEBT-013` is closed or narrowed to a clearly enforced stdlib/server
  compatibility boundary with tests;
- no public `SURGE_SHARDS>1` net path relies on implicit raw-handle owner
  behavior;
- `runtime-v2-check`, `make check`, `make c-check`, `make cppcheck`, applicable
  focused tests, LOC, and Sentrux checks pass or have recorded blockers
  unrelated to Epic 10;
- `DEBT.md`, `NOTES.md`, this document, `README.md`, and `docs/RUNTIME_V2.md`
  are updated with the final state.

## Next Runtime Handoff And Syntax Gate

If Epic 10 closes or narrows the owner-safety and cleanup debts, the next
planning pass can decide whether to enter the explicit Phase 4 crossing surface
or do one more focused test/backend matrix pass first. Any task that changes
Surge syntax, keywords, parser rules, semantic checks, lowering, public
examples, or public crossing APIs must stop first for a dedicated language
syntax review with the user. Names such as `far`, `submit_to`, `crosses`, and
`shard-movable` remain semantic placeholders until that review.
