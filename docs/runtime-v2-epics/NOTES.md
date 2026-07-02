# Runtime V2 Working Notes

This is the live handoff log for Runtime V2 work. Keep it current during each
task, then move durable decisions into the owning epic document before closeout.

## Current State

- Runtime V2 target architecture lives in `docs/RUNTIME_V2.md`.
- Epic documents live in `docs/runtime-v2-epics/`.
- Epic 1 is complete. Its main document is
  `01-contract-rules-harness.md`.
- Task breakdown and status live in `01-contract-rules-harness-tasks.md`.
- Global working rules live in `RULES.md`.
- Tasks 1-5 were committed as `b865472a`:
  `docs(runtime): add Runtime V2 epic planning baseline`.
- Tasks 6-7 were committed as `8ae616a1`:
  `docs(runtime): define Runtime V2 liveness gates`.
- Task 10 evidence is recorded as complete with known debt for the narrow Task
  11 counter-field migration boundary. Task 11 may move or wrap
  `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water` only if it does not change direct handoff,
  `try_send`, sync helper, compensation, ready-drain, or waiter semantics.
- Task 11 implementation is recorded. Channel/blocking compatibility counters
  now live under `rt_shard.channel_blocking_compat`; main-session
  runtime/native `session_end` passed: `5146 -> 5172`, no violations.
- Task 12 CI wiring is recorded. `make runtime-v2-check` now runs the stable
  Runtime V2 seed with `SURGE_SKIP_TIMEOUT_TESTS=0`; the separate CI job
  installs `clang`, `llvm`, `lld`, and `binutils`, sets
  `SURGE_MT_TIMEOUT_SCALE=3`, and runs that target.
- Epic 3 Task 19 structural closeout is recorded. Epic 3 is complete for
  owner-local waiters and dependency-aware runtime refactoring under `N=1`.
  Main-session closeout gates and benchmark/smoke evidence are copied into
  `03-evidence.md`. Post-doc Sentrux closeout scans recorded root `6198`,
  runtime `5195`, and runtime/native `5159`; missing rules were debt at that
  time and are closed by the pre-Epic 4 quality hardening.
- Epic 4 starts with persistent fd registry and net lifecycle proof. Do not
  start from `N>1`, crossing syntax, or cross-shard wake protocol work.
- Epic 4 draft now lives in
  `04-persistent-fd-registry-and-net-lifecycle.md`, with task documents under
  `04-tasks/` and evidence in `04-evidence.md`. The first task is
  `04-tasks/01-kickoff-baseline-and-sentrux.md`.
- Epic 4 keeps the current `poll()` backend first. `epoll`, `kqueue`,
  `io_uring`, accept distribution, `N>1`, crossing syntax, heap counters, and
  the broad VM/native/LLVM test-matrix rewrite remain out of scope.
- Epic 4 implementation tasks must prove fd registration, duplicate readiness
  interest, cancellation, close, stale wake, numeric fd reuse or equivalent
  generation safety, wake-fd notification, shutdown drain behavior, and bounded
  registry-derived polling before closeout.
- Epic 4 draft creation evidence is recorded in `04-evidence.md`.
  `git diff --check` passed. Sentrux draft scans recorded root `6198`,
  runtime `5195`, and runtime/native `5159`; Sentrux rules now exist and pass
  for all three mandatory scan roots.
- Pre-Epic 4 quality hardening is recorded: Sentrux rules now exist for root,
  `runtime/`, and `runtime/native`; CLI and MCP rule checks pass for all three
  paths. `check_file_sizes.sh` now checks `go,c,h` by default, prunes generated
  dirs, and enforces `.loc-legacy-allowlist` for existing native runtime files
  over the hard gate. Durable debt is tracked in `DEBT.md`.
- Task 14 Epic 2 closeout is recorded and approved local gates passed after the
  docs edits. Epic 2 is complete for the `N=1` runtime/shard structure slice:
  no owner-local waiter, persistent fd registry, `N>1`, or crossing-syntax
  implementation is claimed. The broad VM/backend regex remains later
  test/backend debt. Main-session Task 14 Sentrux closeout scans recorded root
  `6207`, runtime `5209`, and runtime/native `5172`; missing Sentrux rules were
  debt at that time and are closed by the pre-Epic 4 quality hardening.
- Epic 3 Task 17 extracted trace and SIGUSR1 dump responsibility from
  `runtime/native/rt_async_state.c` into
  `runtime/native/rt_async_trace.c`. The refactor did not change scheduler,
  waiter, timer, channel, or net semantics. Post-refactor line counts:
  `rt_async_state.c` 1731, `rt_async_trace.c` 497,
  `rt_async_internal.h` 499, and `rt_net.c` 1024.
- Task 13 accessor cleanup is recorded as audit-only. The migrated scheduler,
  net poll scratch, channel compat, and runtime/shard skeleton surfaces are
  clean in current `runtime/native`; no runtime code change was justified.
  `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` passed for the Task 13 docs-only closeout. Main-session
  Sentrux scans recorded root `6207`, runtime `5209`, and runtime/native
  `5172`; missing rules were debt at that time and are closed by the
  pre-Epic 4 quality hardening.
- Task 9 implementation evidence is recorded. Main-session Sentrux runtime/native
  `session_end` passed for this task: `5132 -> 5146`, `signal_delta=14`, no
  violations.
- Latest Task 9 checks passed: `make c-check`, `make cppcheck`, `make check`,
  focused net wake probe, native net benchmark, `git diff --check`, Sentrux
  repository scan, and Sentrux runtime scan. Both Sentrux `check_rules` calls
  reported missing rules files at that time; this is now closed by the
  pre-Epic 4 quality hardening.
- Epic 2 is complete in `02-n1-runtime-shard-structure.md` for the `N=1`
  `rt_runtime`/`rt_shard` structure slice; owner-local waiters, persistent fd
  registry, `N>1`, crossing syntax, and the VM/native/LLVM test-matrix rewrite
  are later epics.
- Epic 2 task files live in `02-tasks/`. Runtime-code tasks are paired with
  test-writing tasks where meaningful tests can be written, and the stable
  Runtime V2 liveness seed is now covered by `make runtime-v2-check` and the
  separate CI job.
- Epic 2 task evidence is recorded in `02-evidence.md`.
- Epic 3 Task 04 added pending waiter behavior contract tests in
  `internal/vm/runtime_v2_waiter_contract_test.go`. The default tag-off gate
  passes with no tests selected. The tagged
  `go test -tags runtime_v2_pending` waiter proof now passes after making the
  `print` default argument explicit in the `.sg` snippets. The earlier
  `rt_string_len_bytes` crash was reclassified as LLVM/default-argument lowering
  debt, not waiter cleanup/stale-wake evidence.
- Epic 3 Task 05 added the default-tag static boundary check
  `internal/vm/runtime_v2_waiter_static_test.go`. It compiles the current
  waiter key/list helper declarations and the legacy executor/task waiter
  storage shape with `clang -fsyntax-only`. It does not execute runtime/native
  code, does not depend on `runtime_v2_pending` behavior tests, and does
  not claim Sentrux rule compliance.
- Subagents now use a plan gate: they must return a plan for approval before
  implementation, test-writing, or review work starts. If no real plan mode is
  available, use a no-edit plan-only prompt and approve the plan explicitly.
- Epic 2 drafting checks passed: `git diff --check`, stale phase/epic wording
  grep, Sentrux repository scan, and Sentrux runtime scan. Sentrux rules are
  still missing at both scan roots and must not be reported as rule compliance.
- Epic 2 Task 1 kickoff evidence is recorded in `02-evidence.md`. It captured
  baseline commit `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`, clean starting
  status on `codex/runtime-net-scheduler-refactor`, accepted VM/backend-test
  debt, root/runtime Sentrux scans, and the missing-rules deferral.
- Epic 2 Task 2 field ownership map is recorded in
  `02-field-ownership-map.md`. It classifies every current `rt_executor` field
  before runtime field movement and names the first code-task field boundary.
- Epic 2 Task 3 CI/test contract is recorded in `02-ci-test-contract.md`. It
  defines the future exact-name Runtime V2 gate and keeps the broad focused
  VM/backend debt out of required green gates.
- Epic 2 Task 4 skeleton-test proof is recorded in
  `internal/vm/runtime_v2_skeleton_static_test.go`. It uses the
  `runtime_v2_pending` build tag and intentionally fails before Task 5 because
  `rt_runtime`, `rt_shard`, the `N=1` count macro, and skeleton accessors do not
  exist yet. The check is local-only until Task 12.
- Epic 2 Task 7 scheduler-shape migration evidence is recorded in
  `02-evidence.md`. Scheduler container fields now live under
  `rt_shard.scheduler`. Current scheduler trace proof uses
  `TestMTWorkStealing` and `TestMTSeededScheduler`. `TestMTSeededScheduler`
  remains in the future CI seed; `TestMTWorkStealing` stays
  local-only/current-runtime evidence because Tier 1 stealing is not a Runtime
  V2 hot-path contract.
- Epic 2 Task 10 channel/blocking compatibility evidence is recorded in
  `02-evidence.md`. Stable direct channel subset and the CI-contract
  channel/blocking pair passed. Native channel before-benchmark passed with
  current-checkout compiler `/tmp/surge-task10.nOjRbh/surge` and wrote
  `build/benchmarks/runtime-v2-task10-native-channel-before.md`. The report is
  ignored under `build/`; selected durable rows were copied into
  `02-evidence.md`.

## Epic 4 Task 01 Handoff

- Scope completed: kickoff baseline and Sentrux state before fd-registry work.
  Docs-only; no runtime, test, Makefile, CI, or Sentrux rule changes.
- Start commit: `05ceb7c2 chore(runtime): enforce Runtime V2 quality gates`;
  branch `codex/runtime-net-scheduler-refactor`; clean tree at start.
- Line counts at kickoff: `rt_net.c` 1024, `rt_async_state.c` 1731,
  `rt_async_trace.c` 497, `rt_async_internal.h` 499, `rt_async_waiter.c` 309,
  `rt_runtime.c` 161.
- Sentrux CLI checks passed for all three roots: root `6198` (10 rules),
  runtime `5195` (7 rules), runtime/native `5159` (7 rules).
  `sentrux gate --save` stored the three baselines. The Sentrux MCP server is
  not connected in this session; CLI `check`/`gate` evidence replaces the MCP
  `session_start`/`session_end` flow for Epic 4 and this is recorded honestly
  in `04-evidence.md`.
- Startup gates: `make c-check` pass, `make cppcheck` pass, `make check`
  pass. `make runtime-v2-check` failed once in the MT seed (known Epic 3
  flake class, `AllowTimersToAdvance` program timeout under load); the full
  isolated rerun passed `exit=0`. Pre-existing flake debt, not an Epic 4
  regression.
- Gate plan for Tasks 2-7 is recorded in `04-evidence.md`.
- Next: Tasks 2-4 may run in parallel with separate write sets. Task 2 edits
  only the map doc; Task 3 edits only
  `internal/vm/runtime_v2_fd_registry_contract_test.go`; Task 4 edits only
  `internal/vm/runtime_v2_fd_registry_static_test.go`. The main session owns
  `04-evidence.md` and `NOTES.md` updates to avoid write conflicts.

## Epic 4 Tasks 02-04 Handoff

- Scope completed: dependency map (Task 2), fd lifecycle contract tests
  (Task 3), and registry static shape tests (Task 4). All three ran as
  plan-gated subagents with approved plans and disjoint write sets; the main
  session recorded evidence and owns the commits.
- New artifacts: `04-fd-registry-dependency-map.md` (390 lines),
  `internal/vm/runtime_v2_fd_registry_contract_test.go` (499 lines,
  `runtime_v2_pending`, 4/4 green twice),
  `internal/vm/runtime_v2_fd_registry_static_test.go` (175 lines,
  `runtime_v2_pending`, Boundary green, Shape expected-red until Task 5).
- Load-bearing map facts: close never wakes parked net waiters and never
  kicks the poller; numeric fd reuse can wake old-lifetime waiters;
  `ex->shutdown` has no writer anywhere in `runtime/native` (no graceful
  shutdown contract exists today); the wake pipe is process-global and
  written only from `park_current` for net keys.
- Approved Task 5 shape contract is pinned by the static Shape test:
  `rt_fd_entry {fd, generation, close_state, want_accept, want_read,
  want_write}`, `rt_fd_registry {entries, len, cap}`, by-value
  `rt_shard.fd_registry`, shard/executor accessors, and
  `rt_fd_registry_init/free/ensure_cap/len/find_const` returning
  `rt_runtime_status` for recoverable failures. Declarations go into a new
  `runtime/native/rt_fd_registry.h` included from `rt_async_internal.h`
  (that header is at 499/500 lines and must not grow past the limit).
- Contract-test assertion durability rule (do not violate in later tasks):
  the four fd contract tests assert only migration-durable counters
  (`io_poll_waiters_max`, `io_poll_calls`, `io_poll_net_ready`,
  `io_direct_waits`, `io_waiter_completed`). Tasks 7 and 12 must keep the
  meaning of `io_poll_waiters_max` as max distinct fd rows per poll build.
- Known behavior fact recorded during Task 3: Surge handle copies
  (`{ __opaque: handle }`) clone the `NetConn` view; after `close`, ops
  through a copy hit `EBADF` and map to `NET_ERR_IO` (8), not
  `NotConnected` (5). This is a live fd-reuse hazard input for Tasks 8-9.
- Caution recorded, outside Epic 4 scope: a scratch LLVM program printing a
  pointer-valued int handle (`conn.__opaque to string` concatenation)
  segfaulted reproducibly while uint error codes print fine. Candidate
  compiler/runtime bug for a later backend task; repro kept in the session
  scratchpad, not in the repo.
- CI note: the new fd contract tests are Task 13 promotion candidates by
  extending the `runtime_v2_pending` run filter in `runtime-v2-waiter-check`.

## Epic 4 Task 05 Handoff

- Scope completed: registry container skeleton (Task 5) as a plan-gated
  subagent. Working tree intentionally left uncommitted; the main session
  owns the commit and the Sentrux CLI check/gate evidence.
- What exists now: `runtime/native/rt_fd_registry.h` (54 lines; types,
  accessor and lifecycle declarations, one ownership comment block) included
  from `rt_async_internal.h` directly after the `rt_shard`/`rt_executor`
  forward typedefs; `runtime/native/rt_fd_registry.c` (72 lines;
  `init`/`free`/`ensure_cap`/`len`/`find_const`); by-value
  `rt_shard.fd_registry` beside `net_poll_scratch`; shard-first accessors
  plus shard0 executor adapters in `rt_runtime.c`; init wired into
  `rt_runtime_init_n1` so the registry initializes with the owning shard and
  status flows through the existing `exec_init_once` failure boundary.
- Line budget resolution: `rt_async_internal.h` stayed at 499 lines
  (+include, +field, -2 blank separator lines). All future fd-registry API
  growth (Tasks 6/7/9 mutators) must land in `rt_fd_registry.h`, which costs
  `rt_async_internal.h` nothing.
- Zero-reader guarantee held: nothing in `rt_net.c`, `rt_async_state.c`, or
  `rt_async_waiter.c` references the registry; the poll rebuild path is
  unchanged. No net behavior change is claimed or possible.
- `rt_fd_registry_free` has no caller by design: `ex->shutdown` still has no
  writer, so no teardown path exists to hook. Tasks 10-11 create the
  shutdown path and wire the free. Do not "fix" the unused free earlier.
- Growth contract (mirrors `rt_waiter_store_ensure_cap`): lazy allocation,
  start cap 16, doubling, `SIZE_MAX` overflow guards, `rt_realloc`, explicit
  `RT_RUNTIME_STATUS_*` codes, no `panic_msg` in the new API.
- Tested: Shape static gate flipped red->green with zero test edits;
  Boundary static gate green; Task 3 contract 4-pack green (15.9s); `make
  c-check`/`cppcheck`/`runtime-v2-check`/`check` green;
  `TestMTNetWaiterWakeupLatency` green (2.37s); `git diff --check` clean.
- Not tested: the registry has no behavior yet, so no liveness/behavior
  proof covers it; `ensure_cap`/`find_const` get their first behavior proof
  when Task 6 registration writes land and extend the Shape gate.
- Next decision before Task 6: mutation API shape for registration-side
  interest writes under `ex->lock` alongside `prepare_park`, plus the Shape
  static gate extension for those mutators.

## Epic 4 Task 06 Handoff

- Scope completed: net wait registration through registry-owned fd entries
  (Task 6) as a plan-gated subagent. Working tree intentionally left
  uncommitted; the main session owns the commit and the Sentrux CLI
  check/gate evidence.
- What exists now: `rt_fd_registry_attach_net_interest` /
  `rt_fd_registry_detach_net_interest` in `rt_fd_registry.h/.c` (63/154
  lines), driven by the `fd-registry-waiter-bridge` statics in
  `rt_async_waiter.c` (381 lines) at the four waiter-store mutation sites:
  attach in `add_waiter`, detach-if-last in `remove_waiter` / `pop_waiter`
  (same-pass `kept_same_key` counting) and in
  `rt_executor_wake_net_waiters_for_key` (remaining 0 by construction). The
  hook placement covers every net park/wake/cancel/rollback path with zero
  changes to `rt_net.c` (1024, flat) and `rt_async_state.c` (1731, flat);
  `rt_async_internal.h` stayed 499 (invariant block edited in place: ex->lock
  now lists fd registry rows).
- The registry has writers but ZERO readers: poll input is still 100%
  waiter-derived (`poll_net_waiters` byte-identical), so net behavior is
  preserved by construction. No new wake was added; `park_current` still owns
  the net wake-pipe kick and `io_cv` signal.
- Row lifetime invariant: a row exists iff at least one net-key waiter for
  that fd is parked. Clearing the last interest flag swap-removes the row.
  Consequence Task 9 MUST NOT miss: remove-plus-recreate resets `generation`
  to 0, so today's rows carry no cross-lifetime generation protection —
  Task 9 owns re-deciding row lifetime when it adds generation/close
  semantics (no generation bumps or close marking exist yet).
- Named bridges for later removal/validation:
  `fd-registry-waiter-bridge` (interest mirrors waiter-store membership;
  re-validate or replace at Tasks 7/9) and `fd-registry-attach-miss`
  (allocation failure -> waiter parks without a row, behavior unchanged;
  Task 7 must resolve it when poll input becomes registry-derived).
- Interest flags stay 0/1 flags per the pinned shape, not counts; the
  last-waiter decision comes from waiter-store scans. Duplicate same-key
  waiters keep interest alive (contract test 3 green).
- `wake_key_all_with_policy` net branch intentionally not hooked: grep
  re-verified zero net-key producers (scope/join/blocking only); stays
  Task 7 dead-path cleanup debt per the dependency map.
- Tested: c-check/cppcheck/runtime-v2-check/check green; extended Shape gate
  green with the new mutator pins; Boundary green with zero edits; Task 3
  contract 4-pack 4/4 (15.7s); `TestMTNetWaiterWakeupLatency` PASS (2.46s);
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer` PASS (4.47s);
  manual debug-gated fixture run (`SURGE_ASYNC_DEBUG=1`) exited 0 with zero
  bridge mismatch/attach-miss lines while parking and completing net waiters.
- Not tested: registry contents are not yet observable by any behavior test
  (no reader exists); the debug recount check ran clean but only anomalies
  print, so its first adversarial proof arrives with the Task 8 fixtures.
  Native net benchmark deferred to Task 7 per the gate plan.
- Next decision before Task 7: poll-set construction from registry rows must
  replace `rt_executor_visit_net_waiters`, decide the `fd-registry-attach-miss`
  resolution (fail the wait vs. fallback), replace the `net_len` capacity
  hint, and delete/unify the dead `wake_key_all_with_policy` net bookkeeping.

## Epic 4 Task 07 Handoff

- Scope completed: poll-from-registry migration (Task 7) as a plan-gated
  subagent. Working tree intentionally left uncommitted; the main session
  owns the commit and the Sentrux CLI check/gate evidence.
- What exists now: `poll_net_waiters` builds its poll set ONLY from registry
  rows — capacity from `rt_fd_registry_len`, scratch filled by
  `rt_fd_registry_snapshot_poll_interest` (one linear pass, rows unique per
  fd, `want_accept` folded into readable-class `want_read`), completion and
  poll-error paths unchanged and running against the ex->lock-held snapshot
  copy. The waiter-scan build (`rt_executor_visit_net_waiters`,
  `collect_net_poll_fd`, `NetPollBuildContext`, `NetPollFd`) is deleted, as
  are dead `rt_executor_waiter_len`, `rt_executor_net_waiter_len`,
  `rt_waiter_key_visitor`, and the `wake_key_all_with_policy` net branch.
  Line counts: `rt_net.c` 1002, `rt_async_waiter.c` 348,
  `rt_async_internal.h` 493, `rt_fd_registry.h` 80, `rt_fd_registry.c` 213,
  `rt_async_state.c` 1727.
- fd-registry-attach-miss bridge is RESOLVED: `net_wait_current_task`
  verifies `rt_fd_registry_net_interest_present` after `prepare_park` and on
  a miss undoes the park (remove_waiter + clear park_prepared/park_key/
  pending_key) under the same lock hold, returning spurious readiness (net
  ops are nonblocking and re-wait). Invariant now load-bearing: a parked net
  waiter ALWAYS has a registry row, so `has_net_waiters` (still
  waiter-derived `net_len`) gating `begin_net_poll` cannot admit a
  zero-row poll cycle (which would busy-spin `rt_io_main` and strand
  `rt_worker_main`/`next_ready`).
- Task 8 fixture target (record from main-agent review): stale registry
  interest (flag set, zero same-key waiters) is now the only route to a
  level-triggered io-loop spin, because a stale row keeps its fd in every
  poll set while completions are no-ops. The `SURGE_ASYNC_DEBUG` bridge
  recount polices it; Task 8 fixtures should hammer duplicate-waiter
  cancel/close orderings to prove the invariant adversarially.
- CI-gate contract updates (main-agent approved, recorded in
  `04-evidence.md`): `TestRuntimeV2NetWaiterTraceContract` now asserts
  `io_waiter_scan_entries==0`, `io_waiter_net_entries==0`,
  `io_poll_dedup_checks==0` (machine-checkable "legacy rebuild path unused"
  evidence) and keeps `io_poll_rebuilds==io_poll_calls`;
  `runtime_v2_waiter_static_test.go` dropped the three deleted-symbol pins.
  Counter NAMES are untouched (Task 12 owns naming); only increment sites
  died with the legacy build. `io_poll_waiters_max` keeps its
  distinct-fd-rows-per-build meaning; fd contract 4-pack byte-identical and
  green.
- Tested: c-check/cppcheck/runtime-v2-check/check green; Shape gate extended
  with `rt_fd_poll_interest` + both Task 7 read APIs; Boundary green with
  zero edits; Task 3 4-pack 4/4 (16.0s); `TestMTNetWaiterWakeupLatency` PASS
  (2.31s); `TestNativeNetSingleThreadBlockingChannelInAsyncServer` PASS
  (4.34s); debug-gated contract pair PASS. Before/after
  `bench_native_net.sh` with pinned scratch compilers (`617f8cfa5881`):
  echo rows flat or better (e.g. 1/echo/seq 65.38 -> 62.14 us/op), scan/net
  entries -> 0 across all 24 rows, allocs flat at 2, rebuilds == calls; the
  `1/manager/seq` outlier in the AFTER report (129.62) was re-run same-binary
  to 110.86 — channel-hop run variance, not a poll regression. No leftover
  benchmark processes after either run (`ps` checks recorded).
- Not tested: attach-miss undo path has no fault-injection proof (allocation
  failure is not reachable from a fixture without an alloc shim); it is
  compile-proven, static-pinned, and its invariant is debug-checked. Close/
  cancel/re-register behavior is Tasks 8-9; generation/close_state still have
  no behavior.
- Main-session Task 7 closeout: Sentrux MCP checks passed for root `6198`,
  runtime `5228`, and runtime/native `5172`; `sentrux gate` passed for all
  three roots (`6198 -> 6198`, `5195 -> 5228`, `5159 -> 5172`). Re-run gates
  passed: `git diff --check`, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, fd static gates, fd contract 4-pack,
  net trace contract, focused net wake probe, single-thread net/channel probe,
  debug-path proof, and native net closeout benchmark
  `build/benchmarks/runtime-v2-task07-closeout-native-net.md`. Sentrux
  baseline files are committed so future `sentrux gate` checks are
  reproducible. `.loc-legacy-allowlist` ceilings were lowered to current
  `rt_async_state.c` 1727 and `rt_net.c` 1002.
- Next decision before Task 8/9: fixtures must cover close-with-parked-waiter
  and fd-reuse stale wake (dependency map hazards 1-2) now that rows are poll
  input; Task 9 re-decides row lifetime (remove-plus-recreate resets
  generation to 0) when close/generation semantics land.

## Epic 4 Task 08 Handoff

- Scope completed: close/cancel/re-register behavior tests. Only
  `internal/vm/runtime_v2_fd_registry_lifecycle_test.go`, Task 8 docs, the
  task index, evidence, and this notes file changed. Runtime/native, Makefile,
  CI, Task 7 code, and existing fd contract tests were not edited.
- New file: `runtime_v2_fd_registry_lifecycle_test.go` (297 lines,
  `runtime_v2_pending`, package `vm_test`) reuses the Task 3/7 helpers for
  LLVM fixture build/run, trace parsing, port allocation, and fd-registry trace
  assertions. The existing 499-line fd contract file stayed unchanged.
- Green now: cancelling one duplicate read waiter preserves the other read
  waiter and permits a later same-fd read re-registration; cancelling read
  interest while a write waiter remains active preserves the write interest.
  Final focused command passed both tests in package time `12.464s`.
- Task 9 expected-red now exists and is precise: closing a listener with a
  parked accept waiter exits `3` with `accept_close_timeout`; closing a
  connection with a parked read waiter while the peer stays open exits `3` with
  `read_close_timeout`. Both fixtures build cleanly and fail through runtime
  behavior only. Their TRACE_NET rows kept `io_waiter_scan_entries=0`,
  `io_waiter_net_entries=0`, and `io_poll_dedup_checks=0`.
- Numeric fd reuse was not added as a Go-only fixture. The Task 8 allowed write
  set excludes a native helper, and the Go/socket surface cannot force numeric
  reuse deterministically enough for CI. Task 9 must prove generation or
  closed-state stale-wake handling, or explicitly expand scope for a
  deterministic helper.
- Checks passed: `gofmt -l`, `go vet -tags runtime_v2_pending ./internal/vm`,
  tag-off fd-registry proof, focused green cancel/re-register tests,
  `TestMTNetWaiterWakeupLatency`, `make runtime-v2-check`, new-file
  whitespace check, `check_file_sizes.sh`, root, runtime, and
  runtime/native Sentrux gates, and `git diff --check`. Review subagent found
  no P0/P1 blockers. The close command is intentionally expected-red until
  Task 9 implements close-owned registry lifecycle.

## Epic 1 Artifacts

- `RULES.md`: global Runtime V2 development rules.
- `SENTRUX_POLICY.md`: Sentrux scan/rule policy and current rule-check
  requirements.
- `EVIDENCE_TEMPLATE.md`: required evidence format for future tasks.
- `01-baseline-evidence.md`: current checkout checks, benchmark reports,
  counters, and blockers.
- `LIVENESS_PROBES.md`: liveness probes by changed runtime surface.
- `OPEN_DECISIONS_BEFORE_EPIC_2.md`: accepted debt, blockers, and deferrals
  before structural `N=1` work.
- `01-contract-rules-harness.md`: durable Epic 1 summary and Epic 2 start
  criteria.

## Durable Decisions

- Work proceeds epic-by-epic. Later epics stay as a short roadmap until earlier
  evidence shapes the next slice.
- Subagents must plan first and wait for approval before edits or review work.
- `MUST` rules block completion, except for documented proving spikes with
  hypothesis, allowed files/surfaces, non-final behavior, proof command,
  success/failure criteria, and rollback.
- Runtime code must stay explainable through ownership, wakeup, cancellation,
  lifetime/generation, backpressure, and trace/test evidence.
- Sentrux is mandatory. Root and scoped scans are required when a task mostly
  affects `runtime/`.
- Runtime V2 code limit is 500 lines for new or heavily rewritten code files.
- New V2 C APIs return explicit status codes for recoverable failures.
  `panic_msg` is not the primitive error-handling contract.
- Channel FIFO, task parking at suspension points, cooperative cancellation,
  structured join/failfast outcomes, and `@local spawn` sendability rules are
  source-visible contracts.
- Native global FIFO waiters, global inject, worker-local queues, Tier 1 work
  stealing, direct channel handoff placement, and sync-channel compensation are
  current implementation artifacts unless a later spec promotes one explicitly.
- VM/native parity means semantic output parity under native `threads=1`, not
  identical scheduler interleavings.

## Current Sentrux Baselines

- Repository scan: `/home/zov/projects/surge/surge`, `quality_signal=6198`.
- Runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5195`.
- Runtime/native scan: `/home/zov/projects/surge/surge/runtime/native`,
  `quality_signal=5159`.
- `check_rules` reports no `.sentrux/rules.toml` for the scanned paths. This is
  not a passing rule check. Runtime-code tasks must add real rules or record an
  explicit temporary deferral without claiming rule compliance.

## Current Baseline Debt

- `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in this checkout when
  timeout-sensitive tests are not skipped.
- Default `make check` passes because `SURGE_SKIP_TIMEOUT_TESTS=1` skips those
  timeout-sensitive VM/LLVM tests through `skipTimeoutTests`.
- The focused VM failure is accepted backend-test debt. A later test/backend
  epic will rewrite the VM/native/LLVM test matrix around stable Runtime V2
  contracts.
- Native net and channel benchmark reports in `build/benchmarks/` were
  regenerated with a temporary current-checkout compiler after a stale `./surge`
  binary was detected.

## Epic 2 Start Blockers

- Sentrux missing-rules status was explicitly deferred by Epic 2 Task 1 without
  claiming rule compliance. Runtime-code tasks still must add real rules or
  record a fresh temporary deferral for the active scan path.
- The first `N=1` task must name the exact behavior equivalence boundary from
  `01-contract-rules-harness.md` and `OPEN_DECISIONS_BEFORE_EPIC_2.md`.
- Epic 2 evidence must keep the focused VM debt named and must not attribute new
  runtime regressions to that debt without proof.

## Epic 2 Task 1 Kickoff Handoff

- Task: `02-tasks/01-kickoff-evidence.md`.
- Scope completed: documentation-only baseline evidence. No runtime,
  compiler, ABI, benchmark, CI, or Sentrux rule-file changes were made.
- Start state: baseline commit
  `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`; branch
  `codex/runtime-net-scheduler-refactor`; `git status --short` was empty before
  the task.
- Sentrux root scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6210`, `files=4740`, `import_edges=1887`, and
  `lines=370800`; health bottleneck remains `modularity`; `check_rules` still
  reports missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5147`, `files=32`, `import_edges=30`, and `lines=14883`;
  health bottleneck remains `redundancy`; `check_rules` still reports missing
  `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`.
- Missing Sentrux rules remain a blocker to claiming rule compliance. Task 1
  records a temporary deferral only; the first runtime-code task must either add
  real rules or record a fresh deferral for the active scan path.
- Accepted VM debt remains unchanged:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` is not an Epic 2 kickoff
  gate and was not run in Task 1. New runtime failures must not be assigned to
  this debt without matching `01-baseline-evidence.md`.
- Approved checks for Task 1: `git diff --check` and `make check`; broad
  focused VM regex, benchmarks, and extra liveness probes are intentionally
  skipped.
- Task 1 checks passed: `git diff --check` produced empty output, and
  `make check` passed in 14.31s. `make check` ran
  `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`,
  `make c-check`, and `check_file_sizes.sh`.
- Next owner: Epic 2 Task 2, Field Ownership Map. It should classify current
  `rt_executor` state before any runtime field movement.

## Epic 2 Task 2 Field Ownership Handoff

- Task: `02-tasks/02-field-ownership-map.md`.
- Scope completed: documentation-only ownership classification. No runtime,
  compiler, ABI, benchmark, CI, Sentrux rule-file, staging, or commit changes
  were made.
- Output: `02-field-ownership-map.md` classifies every `rt_executor` field into
  runtime lifecycle/control plane, `N=1` shard-local hot state,
  compatibility/offload state, trace/debug-facing state, or later-epic state.
- Direct usage searches covered scheduler queues, waiter storage, net poll
  scratch, task/scope registries, lifecycle flags, channel compensation, and
  blocking pool state under `runtime/native`.
- Safe Epic 2 move candidates are runtime lifecycle shell, task/scope registry,
  scheduler queue shape, net poll scratch, and channel/blocking compatibility
  state. Each remains behavior-preserving and must use the matching
  `LIVENESS_PROBES.md` evidence when code moves fields.
- Deferred owners: local-waiter epic for owner-local waiter queues, local
  fd-registry epic for persistent readiness, multi-shard runtime epic for owner
  placement and distributed scope semantics, allocator/pools epic for heap
  counters and hot object pools, and later IO/backend work for backend choice.
- First code-task boundary: introduce the runtime/shard shell around `lock`,
  `ready_cv`, `io_cv`, `done_cv`, `workers`, `worker_ctxs`, `worker_count`,
  `initialized`, `io_started`, `shutdown`, `sched_mode`, and `sched_seed` only.
  Do not move waiters, fd readiness semantics, channel handoff semantics,
  blocking pool queue, or task/scope ownership unless the approved task plan
  expands the field group and evidence.
- File-size risk remains active for `rt_async_state.c`, `rt_net.c`, and
  `rt_async_channel.c`; later runtime-code tasks must avoid growing them or
  record a split/follow-up.
- Approved checks for Task 2: `git diff --check` and the map placeholder sanity
  grep. Runtime tests, benchmarks, liveness probes, and Sentrux scans are
  intentionally skipped for this docs-only task.

## Epic 2 Task 3 CI/Test Contract Handoff

- Task: `02-tasks/03-runtime-v2-ci-test-contract.md`.
- Scope completed: documentation-only CI/test contract. No `Makefile`, GitHub
  Actions, runtime, compiler, benchmark, Sentrux, staging, or commit changes
  were made.
- Output: `02-ci-test-contract.md` defines a future `runtime-v2-check` shape
  that runs exact named tests with `SURGE_BACKEND=llvm` and
  `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Proposed seed tests:
  `TestMTWakeupsAndCancellation`, `TestMTChannelParkUnpark`,
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`, and
  `TestMTSeededScheduler`.
- Proposed Task 12 command:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
    go test ./internal/vm \
      -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
      -v --timeout 120s
  ```

- Required future CI setup: install `clang`, `llvm`, and `lld`; preflight
  `clang` and `ar`; set `SURGE_MT_TIMEOUT_SCALE=3`; keep the Runtime V2 job
  separate from the default skipped-timeout Go matrix.
- Excluded required gate:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`. It remains accepted
  backend-test debt and may be used only as a diagnostic until the later
  test/backend matrix epic fixes or replaces it.
- Local-only until re-proven: net latency, one-worker net/channel
  compatibility, broader channel correctness, structured concurrency, blocking
  pool, heavier sync-helper compensation, compensation-limit stress, and
  current Tier 1 work-stealing probes.
- Candidate Runtime V2 seed and net commands were not run in Task 3. Do not
  report them as fresh passes without Task 12 or task-specific evidence.
- Approved checks for Task 3: `git diff --check` and direct
  `git diff --no-index --check` on the new contract file. Runtime tests,
  `make check`, `make c-check`, `make cppcheck`, benchmarks, and Sentrux scans
  are intentionally skipped for this docs-only task.

## Epic 2 Task 4 Runtime/Shard Skeleton Tests Handoff

- Task: `02-tasks/04-runtime-shard-skeleton-tests.md`.
- Scope completed: added a local-only pending static check for the Task 5
  runtime/shard skeleton. No runtime implementation, `Makefile`, CI workflow,
  benchmark, Sentrux, staging, or commit changes were made.
- New test: `TestRuntimeV2SkeletonStaticShape` in
  `internal/vm/runtime_v2_skeleton_static_test.go`.
- The test is hidden behind `//go:build runtime_v2_pending`; default test runs
  do not see it.
- The test compiles a C snippet with `clang -std=c11 -fsyntax-only` and requires
  `RT_RUNTIME_SHARD_COUNT == 1`, complete `rt_runtime` and `rt_shard` types,
  and the accessors `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- Preflight tools exist: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Expected pre-Task-05 failure was recorded with:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  ```

  It failed with missing `RT_RUNTIME_SHARD_COUNT`, undeclared `rt_runtime` and
  `rt_shard`, and undeclared skeleton accessors. This is the desired proof that
  Task 5 has not been implemented yet.
- Default safety check passed:
  `go test ./internal/vm -run '^$' --timeout 30s` returned
  `ok surge/internal/vm (cached) [no tests to run]`.
- `git diff --check` passed after the test and docs edits.
- Task 5 should make this pending check pass as part of skeleton implementation
  or record a blocker unrelated to Task 5 code. Task 12 owns deciding whether
  this exact tagged check or a non-pending successor joins `runtime-v2-check`.

## Epic 2 Task 5 Runtime/Shard Skeleton Handoff

- Task: `02-tasks/05-runtime-shard-skeleton.md`.
- Scope completed: added the internal `N=1` `rt_runtime`/`rt_shard` skeleton
  and accessors required by Task 4. No public ABI, `N>1`, waiter, fd registry,
  scheduler, net poll, channel/blocking, compiler, benchmark, CI, Sentrux rule,
  staging, or commit changes were made.
- Runtime shape: `RT_RUNTIME_SHARD_COUNT == 1`; `rt_runtime` owns
  `shards[RT_RUNTIME_SHARD_COUNT]`; `rt_shard` links to the runtime and current
  executor; `rt_executor` gained only `rt_runtime* runtime`.
- Required accessors now exist: `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- New skeleton init uses `rt_runtime_status`. `exec_init_once()` still preserves
  the legacy `pthread_once`/`panic_msg` boundary because it cannot return an
  init status to callers.
- File-size result: `rt_async_internal.h` is `432` lines, new
  `rt_runtime.c` is `64` lines, and over-limit `rt_async_state.c` was reduced
  from `2391` to `2368` lines by moving cold default worker-count helpers.
- Checks passed:

  ```bash
  git diff --check
  command -v clang
  command -v ar
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  make c-check
  make cppcheck
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make check
  ```

- One local failure happened and was fixed inside Task 5: the first
  `make c-check` run showed `rt_async_state.c` still needed `<unistd.h>` for
  existing trace `write()` calls after CPU-count detection moved.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5147 -> 5144`, delta `-2`, summary `Quality stable or improved`, and no
  violations. A worker-context `session_end` could not reuse that baseline.
- Post-change root Sentrux: `/home/zov/projects/surge/surge`,
  `quality_signal=6209`, bottleneck `modularity`, rules file missing.
- Post-change runtime Sentrux: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5144`, bottleneck `redundancy`, rules file missing.
- Missing Sentrux rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow skeleton implementation.

## Epic 2 Task 6 Scheduler Shape Tests Handoff

- Task: `02-tasks/06-scheduler-shape-tests.md`.
- Scope completed: selected and ran existing scheduler and CI-shaped liveness
  proofs before scheduler field movement. No runtime C, Go test, `Makefile`,
  GitHub Actions, STATS, benchmark, task-doc, Sentrux, staging, or commit
  changes were made.
- Scheduler trace proof command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  ```

  Both `TestMTWorkStealing` and `TestMTSeededScheduler` ran and passed.
- CI-shaped Runtime V2 seed command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  ```

  All four exact tests ran and passed.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- CI ownership: `TestMTSeededScheduler` remains in the future Runtime V2 seed.
  `TestMTWorkStealing` remains local-only/current-runtime evidence and must not
  join the seed unless a later Tier 2 CPU-pool decision promotes stealing.
- Parked-with-work remains a missing invariant. Task 6 did not add a weak
  nondeterministic test.
- Task 7 may proceed only if it preserves current wake elision, worker sleep
  rules, and shard park state. If Task 7 needs to change any of those, it must
  stop and add a real parked-with-work invariant first.
- `git diff --check` passed after the documentation updates.
- Verification note: do not run overlapping `go test ./internal/vm` commands
  that include the same MT test names. The test artifact directory is keyed by
  test name under `target/debug/.tests/`, so parallel runs can collide while
  writing artifacts and create a false failure unrelated to runtime behavior.

## Epic 2 Task 7 Scheduler Shape Migration Handoff

- Task: `02-tasks/07-scheduler-shape-migration.md`.
- Scope completed: moved only scheduler container fields behind the existing
  `N=1` `rt_shard.scheduler`: `inject`, `local_queues`, `worker_ctxs`,
  `worker_count`, `running_count`, `sched_mode`, and `sched_seed`.
- Preserved executor/global lifecycle state on `rt_executor`: `workers`,
  `ready_cv`, `io_cv`, `done_cv`, `lock`, `shutdown`, `net_polling`,
  `initialized`, `io_started`, `channel_blocked_workers`,
  `compensation_count`, `compensation_high_water`, and blocking-pool fields.
- No `runtime/native/rt.h`, `Makefile`, CI, Go test, benchmark script,
  Sentrux rule, net/channel/waiter/task ownership semantic, public ABI,
  staging, or commit changes were made.
- Direct moved-field audit passed with no matches:

  ```bash
  rg -n -- 'ex->(inject|local_queues|worker_ctxs|worker_count|running_count|sched_mode|sched_seed)\b|exec_state\.(sched_seed|sched_mode)' runtime/native
  ```

  `rg` returned exit `1`, the expected no-match status.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Final checks passed:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make c-check
  make cppcheck
  make check
  ```

- A first `make cppcheck` run found const-pointer style warnings in
  `rt_async_state.c`; the declarations were narrowed and the final standalone
  `make cppcheck` passed.
- Sentrux post-change root scan: `/home/zov/projects/surge/surge`,
  `quality_signal=6207`, bottleneck `modularity`, rules file missing.
- Sentrux post-change runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5168`, bottleneck `redundancy`, rules file missing.
  Supplied runtime baseline was `5125`, so the scoped signal increased by `43`.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5125 -> 5168`, delta `+43`, summary `Quality stable or improved`, and no
  violations. Missing rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow shape migration.
- Parked-with-work remains a missing invariant. Task 7 did not change wake
  elision, worker sleep rules, or shard park state, so it did not cross the
  Task 6 boundary.
- Next task: Task 8 must record net poll scratch before-evidence. Run
  `TestMTNetWaiterWakeupLatency` with `SURGE_SKIP_TIMEOUT_TESTS=0`, run the
  native net benchmark with a current-checkout `SURGE` binary and an outer
  timeout, and keep persistent fd registry behavior out of scope. Task 9 should
  not start until Task 8 evidence exists.

## Epic 2 Task 8 Net Poll Scratch Tests Handoff

- Scope completed: recorded net wake and native net benchmark before-evidence.
  No runtime C, Go test, script, `Makefile`, CI workflow, Sentrux rule, STATS,
  task-doc, staging, or commit changes were made.
- Temp compiler was built outside the repository at
  `/tmp/surge-task08.zkEoYd/surge`. Its `version --full --format json`
  `git_commit` matched current `HEAD`: `49b3aa34ec26`.
- Version line recorded:

  ```text
  surge 0.1.13-dev — "forge storms before they land"
  commit: 49b3aa34ec26
  message: refactor(runtime): move scheduler state under shard
  built:  2026-06-26T12:41:59Z
  ```

- Net wake probe passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s
  ```

  The test ran and passed in package time `2.647s`. It did not print trace rows
  on success; it asserted the `TRACE_NET` and `TRACE_EXEC_SNAPSHOT` rows
  internally from child stderr.
- Native net benchmark passed with an outer timeout:

  ```bash
  tmpdir=/tmp/surge-task08.zkEoYd
  SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task08-native-net-before.md" \
    timeout 120s env SURGE="$tmpdir/surge" ./scripts/bench_native_net.sh
  ```

  Report path:
  `/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task08-native-net-before.md`.
- Key benchmark invariants from the full 24-row report: task-context blocking
  sends, task-context blocking recvs, compensation started, and compensation
  high-water stayed `0`; `poll allocs` stayed `2`; `dedup checks` stayed `0`.
- Test decision: no new semantic test is needed for Task 9 if it only moves
  `net_poll_fds`, `net_poll_fds_cap`, `net_poll_pfds`, and
  `net_poll_pfds_cap` behind the `N=1` shard/container and preserves
  rebuild-from-waiters semantics.
- Task 9 must stop for a revised plan if it changes waiter ownership,
  persistent fd registration, readiness lifetime, accept ownership, poll
  ownership, or net wake placement.
- CI ownership: `TestMTNetWaiterWakeupLatency` remains local-only Task 8/9
  evidence unless Task 12 re-proves CI stability. The native net benchmark
  remains manual before/after evidence and should not join CI.

## Task 9 Handoff

- Scope completed: moved only `net_poll_fds`, `net_poll_fds_cap`,
  `net_poll_pfds`, and `net_poll_pfds_cap` out of `rt_executor` and into
  `rt_shard.net_poll_scratch`.
- Implementation shape: added `rt_net_poll_scratch`, added
  `rt_shard_net_poll_scratch()` / `rt_executor_net_poll_scratch()`, and changed
  `ensure_net_poll_fds()` / `ensure_net_poll_pfds()` to grow the shard scratch
  buffers.
- Preserved behavior: `poll_net_waiters()` still derives capacity from
  `ex->net_waiters_len`, scans `ex->waiters`, deduplicates fds in the existing
  nested loop, calls `poll()`, and completes read/accept/write waiters through
  the same keys.
- Explicitly not moved or changed: `net_waiters_len`, `net_polling`, `io_cv`,
  waiter ownership, accept ownership, wake fd placement, fd registry/readiness
  lifetime, public ABI, compiler code, benchmark scripts, Makefile, CI, Sentrux
  rules, and STATS.
- Static audit results:
  - No `net_poll_fds` / `net_poll_pfds` fields remain inside `struct
    rt_executor`.
  - `struct rt_shard` now owns `rt_net_poll_scratch net_poll_scratch`.
  - No direct `->net_poll_fds`, `->net_poll_fds_cap`, `->net_poll_pfds`, or
    `->net_poll_pfds_cap` usage remains under `runtime/native`.
  - Zero-context runtime diff has no changed lines mentioning `net_waiters_len`,
    `net_polling`, `io_cv`, waiters, net wake fd placement, fd registry,
    `epoll`, `kqueue`, `io_uring`, `eventfd`, or accept ownership.
- Focused net wake probe passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s`.
- Current-checkout compiler pin passed with temporary binary
  `/tmp/surge-task09-final.aqFZBL/surge`; both current and reported commits
  were `b48f58ec84e0`.
- Native net after-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task09-native-net-after.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark invariants from the full 24-row report: task-context blocking sends,
  task-context blocking recvs, compensation started, and compensation high-water
  stayed `0`; `poll allocs` stayed `2`; `dedup checks` stayed `0`.
- Gates passed locally: `make c-check`, `make cppcheck`, and `make check`.
- Main-session Sentrux runtime/native `session_end` passed against the pre-task
  baseline: `signal_before=5132`, `signal_after=5146`, `signal_delta=14`, no
  violations. Root scan stayed `6207`; required runtime policy scan ended at
  `5182`; runtime/native scan ended at `5146`.
- Missing root and runtime Sentrux rules remain baseline debt. This is not a
  passing rules gate.

## Task 10 Handoff

- Scope completed: evidence/docs only. No runtime/native code, Go tests,
  scripts, `Makefile`, CI, Sentrux rules, STATS, public ABI, compiler code,
  staging, or commits were changed.
- Completion state: complete with known debt for the narrow Task 11
  counter-field migration boundary only.
- Task 11 allowed boundary: move or wrap field ownership for
  `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water`, and preserve their trace-facing accessors.
- Task 11 must stop for a revised plan before changing compensation semantics,
  sync helper behavior, direct `try_send` or handoff behavior, ready-work
  draining at the compensation limit, channel waiter semantics, or channel
  close/cancellation behavior.
- Stable direct channel subset passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMT(RecvAckHandoffCompletesSenderAfterNonYieldingReceiver|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver|BufferedBlockingRecvRefillWakesSender|ChannelParkUnpark)$'
  -v --timeout 120s -count=1 -parallel=1 -p=1`.
- CI-contract channel/blocking pair passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMT(ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance)$' -v
  --timeout 120s -count=1 -parallel=1 -p=1`.
- Broader sync fallback local-only probe did not pass:
  `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timed out at
  their internal 10-second program timeout; `AllowTimersToAdvance` passed.
- Known direct handoff debt: `TestMTNonYieldingTrySendHandoffWakesReceiver`
  times out when run alone at `SURGE_MT_TIMEOUT_SCALE=1` and `3`. This blocks
  Task 11 only if Task 11 changes direct `try_send`, handoff placement, or
  wake-before-park behavior.
- Current-checkout compiler pin passed for temporary binary
  `/tmp/surge-task10.nOjRbh/surge`; both current and reported commits were
  `8ef946f6cc9e`.
- Native channel before-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task10-native-channel-before.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark trace baseline: all 20 Runtime Trace rows had required
  channel/fallback fields and no `n/a` values. `channel_reused_reply` and
  `channel_new_reply` kept blocking and compensation counters at `0` with
  `handoff yields=19999`. `channel_sync_new_reply` recorded `5000`
  task-context blocking sends and `5000` task-context blocking recvs in every
  mode; channel blocking waits were `0` in mode `1` and nonzero in multi-worker
  or default modes. Compensation stayed `0` for every benchmark row.
- Future owners: direct channel handoff / `try_send` task for the non-yielding
  handoff timeout, sync-helper compensation/liveness task for
  `DoNotParkWorkers`, compensation-limit and ready-drain task for
  `DrainReadyWorkAtCompensationLimit`, and the later local channel-waiter epic
  for close/cancellation and waiter cleanup matrices.

## Task 11 Handoff

- Scope completed: moved `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water` out of `rt_executor` and under
  `rt_shard.channel_blocking_compat`. Added shard/executor compatibility
  accessors mirroring the scheduler and net scratch accessor shape.
- Runtime files changed: `runtime/native/rt_async_internal.h`,
  `runtime/native/rt_runtime.c`, and `runtime/native/rt_async_state.c`.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md` and this file.
- Strictly untouched: `runtime/native/rt_async_channel.c`,
  `runtime/native/rt_async_blocking.c`, `runtime/native/rt.h`, Go tests,
  scripts, `Makefile`, CI, Sentrux rules, STATS, public ABI, and compiler code.
- Behavior boundary: no direct `try_send` or handoff changes, no sync-helper
  behavior changes, no compensation semantic changes, no ready-work draining at
  the compensation limit, no channel waiter semantics changes, and no channel
  close/cancellation changes.
- Static audit results:
  - No `channel_blocked_workers`, `compensation_count`, or
    `compensation_high_water` fields remain inside `struct rt_executor`.
  - `struct rt_shard` now owns
    `rt_channel_blocking_compat channel_blocking_compat`.
  - No direct `ex->channel_blocked_workers`, `ex->compensation_count`,
    `ex->compensation_high_water`, or matching `exec_state.*` usage remains
    under `runtime/native`.
  - Forbidden-surface diff for channel protocol, blocking pool, ABI, tests,
    scripts, `Makefile`, CI, STATS, and compiler paths was empty.
- Gates passed locally: stable direct channel subset, CI-contract
  channel/blocking pair, current-checkout native channel benchmark,
  `make c-check`, `make cppcheck`, `make check`, and `git diff --check`.
- Current-checkout compiler pin passed for temporary binary
  `/tmp/surge-task11-final.86ZWJ8/surge`; both current and reported commits
  were `ec640a47b449`.
- Native channel after-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task11-native-channel-after.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark trace evidence: all 20 Runtime Trace rows had required
  channel/fallback fields and no `n/a` values. Async request/reply probes kept
  blocking and compensation counters at `0`; `channel_sync_new_reply` recorded
  `5000` task-context blocking sends and `5000` task-context blocking recvs in
  every mode. Compensation started and compensation high-water stayed `0` for
  every benchmark row.
- Known-debt tests intentionally not run in Task 11:
  `TestMTNonYieldingTrySendHandoffWakesReceiver`,
  `TestMTBlockingChannelHelpersDoNotParkWorkers`, and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`.
- Sentrux evidence: root scan `6207`, runtime scan `5209`, runtime/native scan
  `5172`, and main-session runtime/native `session_end` passed
  `5146 -> 5172` with no violations. All three `check_rules` calls still
  report missing `.sentrux/rules.toml`; this remains debt, not compliance.

## Task 12 Handoff

- Scope completed: added an explicit Runtime V2 liveness gate in `Makefile` and
  a separate GitHub Actions job outside the existing Go matrix.
- Files changed: `Makefile`, `.github/workflows/ci.yml`,
  `docs/runtime-v2-epics/02-ci-test-contract.md`, `02-evidence.md`, and this
  file; `02-n1-runtime-shard-structure.md` received the matching status update.
- `make runtime-v2-check` preflights `clang` and `ar`, then runs:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -count=1 -parallel=1 -p=1 -v --timeout 120s
  ```

- CI job details: installs `clang`, `llvm`, `lld`, and `binutils`; sets
  `SURGE_MT_TIMEOUT_SCALE=3`; runs `make runtime-v2-check`.
- Local checks passed:
  - `make runtime-v2-check`: all four exact seed tests ran and passed; package
    time `7.427s`.
  - `make check`: passed; default path still used `SURGE_SKIP_TIMEOUT_TESTS=1`,
    then `golangci-lint`, nested `make c-check`, and the file-size check.
- Main-session verification caught and fixed the first target shape before
  commit: without explicit scale/serialization, `TestMTBlockingChannelHelpersAllowTimersToAdvance`
  hit its internal `program timeout after 10s`.
- Sentrux evidence: root scan `6207`, runtime scan `5209`; both `check_rules`
  calls still report missing `.sentrux/rules.toml`, which remains debt rather
  than compliance.
- Default `make check` was not changed.
- The broad accepted-debt command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` was not added as a green gate.
- Not promoted to CI in Task 12: `TestMTWorkStealing`,
  `TestMTNetWaiterWakeupLatency`, `TestRuntimeV2SkeletonStaticShape`, and the
  heavier known-debt channel/blocking stress probes.
- Review risk: repository branch protection may need to require the new
  `Runtime V2 liveness (llvm)` job name separately.

## Task 13 Handoff

- Scope completed: audited the migrated Epic 2 accessor surfaces from Tasks 05,
  07, 09, and 11, then recorded static gate evidence.
- Result: audit-only. No `runtime/native` code change was justified.
- Runtime files inspected: `runtime/native/rt_async_internal.h`,
  `runtime/native/rt_runtime.c`, `runtime/native/rt_async_state.c`,
  `runtime/native/rt_async_task.c`, and `runtime/native/rt_net.c`.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md`, this file, and
  `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`.
- Static audit results:
  - No old scheduler fields remain as `ex->inject`, `ex->local_queues`,
    `ex->worker_ctxs`, `ex->worker_count`, `ex->running_count`,
    `ex->sched_mode`, `ex->sched_seed`, or matching `exec_state.*` access.
  - Scheduler users resolve through `rt_executor_scheduler*()` or
    `rt_shard_scheduler*()` before using local `scheduler->...` fields.
  - No old net poll scratch fields remain as `ex->net_poll_*` or
    `exec_state.net_poll_*` access. `poll_net_waiters()` resolves scratch via
    `rt_executor_net_poll_scratch(ex)`.
  - No old channel/blocking compatibility counters remain as
    `ex->channel_blocked_workers`, `ex->compensation_count`,
    `ex->compensation_high_water`, or matching `exec_state.*` access.
  - Direct runtime/shard container access is confined to `rt_runtime.c`, the
    owner/accessor implementation.
  - `rt_runtime_shard_count` was retained. It is used by
    `TestRuntimeV2SkeletonStaticShape` under `runtime_v2_pending`, so it is an
    intentional skeleton surface rather than an unused helper.
- Gates passed locally: `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, and `git diff --check`.
- Sentrux evidence: main-session scans recorded root `6207`, runtime `5209`,
  and runtime/native `5172`. All three `check_rules` calls still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Strictly untouched: runtime/native code, Go tests, scripts, `Makefile`, CI,
  Sentrux rules, STATS, public ABI, scheduler/net/channel/blocking semantics,
  owner-local waiters, persistent fd registry, `N>1`, and crossing syntax.

## Task 14 Closeout Handoff

- Scope completed: closed Epic 2 documentation for the `N=1`
  runtime/shard-structure slice and recorded final local gates.
- Docs changed: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`,
  `02-evidence.md`, this file, and `README.md`.
- No runtime/native code, Go tests, `Makefile`, CI, scripts, Sentrux rules,
  `STATS.md`, staging, or commit changes were made by this executor.
- Closeout audit found no owner-local waiter, persistent fd registry, `N>1`,
  or crossing-syntax implementation in the Epic 2 runtime/compiler surfaces.
- CI status: `make runtime-v2-check` is the stable local target, and the
  separate GitHub Actions job runs the same seed with timeout-sensitive tests
  enabled. Default `make check` still uses `SURGE_SKIP_TIMEOUT_TESTS=1` and is
  not proof for the broad timeout-sensitive VM/LLVM matrix.
- Sentrux status: main-session closeout scans recorded root `6207`, runtime
  `5209`, and runtime/native `5172`. All three `check_rules` calls still report
  missing `.sentrux/rules.toml`; this remains debt, not compliance.
- Final local gates passed: `make runtime-v2-check`, `make check`,
  `make c-check`, `make cppcheck`, and `git diff --check`.

## Epic 3 Starting Point

- Start with owner-local waiter design, still under `N=1`. Do not combine it
  with persistent fd registry work, `N>1`, accept ownership, or crossing syntax.
- First docs task: map every current waiter user from
  `runtime/native/rt_async_internal.h`, `rt_async_state.c`,
  `rt_async_channel.c`, `rt_async_task.c`, `rt_async_poll.c`,
  `rt_async_scope.c`, `rt_net.c`, and `rt_async_blocking.c`.
- First proof target: owner cleanup, stale wake prevention, cancellation and
  timeout interaction, waiter lifetime/generation, and the current global FIFO
  behavior that must either remain intentional or be demoted from contract to
  implementation detail.
- Gate shape: keep `make runtime-v2-check`, `make check`, `make c-check`,
  `make cppcheck`, and focused `LIVENESS_PROBES.md` commands. Do not promote
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` to a green gate until the
  later test/backend matrix epic replaces that debt.
- Sentrux: record scans honestly. Missing rules are not compliance.

## Liveness Requirements

- Runtime-code tasks cannot close with "watch for hangs" as evidence.
- Use `LIVENESS_PROBES.md` to choose probes by changed surface.
- Missing probes that block owning future work include parked-with-work
  invariant, owner-local waiter cleanup tests, fd-registry lifecycle test,
  channel close/cancellation race matrix, native shutdown liveness, cross-shard
  wake-fd elision, cross-shard cancellation generation, and per-probe timeout
  wrappers for channel benchmarks.

## Known Large Files

These files already exceed the 500-line Runtime V2 limit and need care when
touched:

- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_async_task.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

Touching an over-limit file must record whether the task reduces it, keeps it
flat, or creates a follow-up split task.

## Dead Ends And Cautions

- Do not tune scheduler behavior by machine-specific constants as durable
  design.
- Do not let proving-spike code become architecture without rewriting it into
  rule-compliant form.
- Do not use `TestMTWorkStealing` as a future Tier 1 contract without deciding
  whether the assertion moves to explicit Tier 2 work.
- Do not treat missing Sentrux rules as a passing rules gate.
- Do not treat default `make check` as proof that timeout-sensitive VM/LLVM
  liveness and parity tests pass.
- Do not spend Epic 2 capacity rewriting the semi-broken backend test matrix;
  that belongs to a later dedicated test/backend epic.

## Epic 3 Draft Handoff

- Drafted Epic 3 as
  `docs/runtime-v2-epics/03-owner-local-waiters-and-runtime-refactor.md`.
- Added `docs/runtime-v2-epics/03-evidence.md` and brief task scopes under
  `docs/runtime-v2-epics/03-tasks/`.
- Epic 3 scope: owner-local waiter storage under `N=1`, with no persistent fd
  registry, no `N>1`, no accept ownership, and no crossing syntax.
- Refactoring is now a first-class Epic 3 track. It must be dependency-aware:
  behavior proof first, dependency cluster recorded before extraction, no
  mixed refactor/behavior commits, and no dead-code deletion without reference,
  build, test, and Sentrux evidence.
- Current line-count pressure recorded in the epic:
  `rt_async_state.c` 2431 lines, `rt_net.c` 1040,
  `rt_async_task.c` 768, `rt_async_channel.c` 549, and
  `rt_async_internal.h` 460.
- First Epic 3 implementation task remains Task 01, the kickoff baseline and
  Sentrux evidence. No runtime code has been changed by the draft.
- Subagent plan gate remains required. A read-only explorer plan for waiter and
  refactor analysis was approved and completed with no file edits.
- Subagent confirmed Runtime V2-relevant pressure in `rt_async_state.c`,
  `rt_net.c`, `rt_async_task.c`, `rt_async_channel.c`, and
  `rt_async_internal.h`. It also noted larger non-waiter files such as
  `rt_term.c` and `rt_fs.c`; keep those out of Epic 3 unless touched by waiter
  work.
- Dead-code seed for Task 03: `rt_select_poll_tasks` is suspect only. It has
  native, ABI, and LLVM builtin references, while current select emission
  appears to use `rt_select_poll`. Do not delete it without generated-IR search,
  ABI review, focused tests, and Sentrux evidence.
- Draft verification: `git diff --check` passed. Sentrux root scan
  `/home/zov/projects/surge/surge` reported `quality_signal=6207`; scoped
  runtime scan `/home/zov/projects/surge/surge/runtime` reported
  `quality_signal=5209`. Both `check_rules` calls still report missing
  `.sentrux/rules.toml`, which remains debt rather than compliance.

## Epic 3 Task 01 Handoff

- Scope completed: kickoff baseline and Sentrux state before implementation.
- Start commit: `f4f83c4d docs(runtime): draft Runtime V2 waiter epic`.
- Working tree was clean at task start.
- Runtime/native line-count pressure at start:
  `rt_async_state.c` 2431, `rt_term.c` 1091, `rt_net.c` 1040,
  `rt_fs.c` 978, `rt_async_task.c` 768, `rt_string.c` 762,
  `rt_async_channel.c` 549, and `rt_async_internal.h` 460.
- Startup gates passed: `make runtime-v2-check`, `make c-check`,
  `make cppcheck`, and `make check`.
- Sentrux baseline:
  - root `/home/zov/projects/surge/surge`: `quality_signal=6207`;
  - runtime `/home/zov/projects/surge/surge/runtime`: `quality_signal=5209`;
  - native `/home/zov/projects/surge/surge/runtime/native`:
    `quality_signal=5172`;
  - `session_start` saved the native scan baseline.
- `check_rules` still reports missing `.sentrux/rules.toml` for root, runtime,
  and runtime/native. This is debt, not compliance.
- Accepted backend-test debt remains unchanged: do not promote
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` to a green gate.

## Epic 3 Tasks 02-03 Handoff

- Scope completed: read-only waiter dependency map and refactor/dead-code audit.
- Created `docs/runtime-v2-epics/03-waiter-dependency-map.md`.
- Created `docs/runtime-v2-epics/03-refactor-audit.md`.
- Updated `docs/runtime-v2-epics/03-tasks/README.md` to mark Tasks 02 and 03
  complete.
- No runtime/native code changed.
- Key waiter map facts:
  - current waiter storage is executor-global under `ex->lock`;
  - `wake_token` guards wake-before-park;
  - `net_waiters_len` is a polling hint, not owner-local storage and not an fd
    registry;
  - shutdown-adjacent waiter cleanup has no scoped contract yet;
  - FIFO-by-key remains an open decision before owner-local storage changes.
- Refactor audit result:
  - first safe tranche is waiter key/list extraction into a cohesive waiter
    module, with storage still executor-global;
  - do not move `wake_task_with_policy`, `wake_key_all_with_policy`,
    `park_current`, `clear_select_timers`, net polling, channel handoff, or
    task/select ABI in the first extraction;
  - no proven-dead code was found;
  - `rt_select_poll_tasks` remains suspect-only and must stay until generated
    IR search, ABI review, focused select tests, static gates, and Sentrux
    evidence prove deletion is safe.

## Epic 3 Task 05 Handoff

- Scope completed: added a default-tag static boundary check for the pre-Task 06
  waiter helper extraction seam.
- Created `internal/vm/runtime_v2_waiter_static_test.go`.
- The test asserts the current `rt_executor` waiter storage fields, `rt_task`
  prepared-waiter cleanup fields, `waker_key`/`waiter` storage shape, and helper
  declarations that Task 06 may move into a cohesive waiter module.
- No `runtime/native` files changed.
- The `runtime_v2_pending` waiter behavior tests belong to Task 04 evidence and
  were not used as a Task 05 gate.
- Task 05 checks passed: focused static Go test, `make c-check`,
  `make cppcheck`, and `git diff --check`.
- Sentrux was not run for Task 05, and no missing-rules status is reported as
  compliance.

## Epic 3 Task 06 Handoff

- Scope completed: extracted the legacy waiter key/list helper tranche into
  `runtime/native/rt_async_waiter.c` while preserving executor-global waiter
  storage and task-local wait-key storage.
- Moved helpers: waker key constructors/classification, private net waiter
  accounting for add/remove/pop paths, waiter capacity, wait-key capacity,
  add/remove/clear waiters, wait-key registration, `prepare_park`, and
  `pop_waiter`.
- Kept in `rt_async_state.c`: `park_current`, `wake_task_with_policy`,
  `wake_key_all_with_policy`, `clear_select_timers`, net polling, channel
  handoff, task/select ABI, and all storage fields.
- Header change was limited to `waker_is_net()` because `park_current()` still
  needs net-key classification after the extraction.
- `wake_key_all_with_policy()` retains the same `net_waiters_len` decrement
  inline so `net_waiters_removed()` can stay private to the waiter module.
- Line counts after closeout: `rt_async_state.c` 2431 -> 2212,
  `rt_async_waiter.c` new at 226, `rt_async_internal.h` 460 -> 461,
  `03-evidence.md` 270 -> 381, `NOTES.md` 912 -> 947, and
  `03-tasks/README.md` 41 -> 41.
- Checks passed: `clang-format -i runtime/native/rt_async_waiter.c`,
  `git diff --check`, `make c-check`, `make cppcheck`, rerun
  `make runtime-v2-check`, `make check`, cancellation/join/timeout smoke,
  `TestMTCorrectnessChannels`, and `TestMTNetWaiterWakeupLatency`.
- `make runtime-v2-check` first timed out once in
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`; an isolated rerun passed,
  then the full target passed.
- Direct channel LLVM probe kept known debt visible:
  `TestMTNonYieldingTrySendHandoffWakesReceiver` timed out after 10s; the other
  four listed direct-channel tests passed. The default-backend command passed
  only because all five MT tests skipped under VM.
- Sentrux post-change scans: root `6215`, runtime `5264`, runtime/native
  `5227`. Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.

## Epic 3 Task 07 Handoff

- Scope completed: added a pending owner-local waiter skeleton static check.
- Created `internal/vm/runtime_v2_owner_local_waiter_static_test.go` behind
  `runtime_v2_pending`.
- The pending C shape probe expects `rt_waiter_store` with `entries`, `len`,
  `cap`, and `net_len`; `rt_shard.waiter_store`; and the approved
  `rt_shard_waiter_store*` / `rt_executor_waiter_store*` accessor surface.
- The check fails before Task 08 with unknown `rt_waiter_store`, missing waiter
  store accessors, and no `rt_shard.waiter_store` member.
- The default waiter static test was intentionally not changed; it still checks
  current executor-global waiter storage until Task 08 moves the shape.
- No `runtime/native` files changed.
- Task 07 checks run: expected failing pending proof and passing default static
  safety check. `git diff --check` and `make check` are recorded in
  `03-evidence.md`.
- Task 08 must add the owner-local waiter store under the single shard, keep
  compatibility wrappers, and then update or promote the default static shape
  check.

## Epic 3 Task 08 Handoff

- Scope completed: moved waiter storage behind `rt_shard.waiter_store` under
  the existing `N=1` runtime shape.
- Added `rt_waiter_store` with `entries`, `len`, `cap`, and `net_len`; added
  the shard/executor waiter-store accessors approved in Task 07; removed direct
  waiter storage fields from `rt_executor`.
- Kept compatibility helpers: `add_waiter`, `remove_waiter`, `pop_waiter`,
  `prepare_park`, `clear_wait_keys`, `add_wait_key`, and `ensure_waiter_cap`
  remain the caller-facing helper surface.
- Added `rt_waiter_store_ensure_cap()` with explicit `rt_runtime_status`
  results. The compatibility wrapper keeps the old panic-on-allocation-failure
  behavior.
- Routed remaining direct users in `rt_async_state.c` and `rt_net.c` through the
  store. Net polling still rebuilds scratch from the current waiter list, and
  `net_len` remains a hint, not an fd registry.
- No `N>1`, fd registry, crossing syntax, channel semantic, net semantic, or
  public ABI change was added.
- Updated the default waiter static proof to the owner-local shape. The Task 07
  pending owner-local proof now passes.
- Direct-field audit passed: no `->waiters`, `->waiters_len`, `->waiters_cap`,
  or `->net_waiters_len` uses remain in `runtime/native` or `internal/vm`.
- Checks passed: owner-local pending static proof, default waiter static proof,
  pending waiter behavior proof, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, and `make check`.
- Sentrux post-change scans: root `6206`, runtime `5220`, runtime/native
  `5184`. Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Line counts after closeout: `rt_async_internal.h` 471, `rt_runtime.c` 161,
  `rt_async_waiter.c` 252, `rt_async_state.c` 2221, `rt_net.c` 1042,
  `runtime_v2_waiter_static_test.go` 82, and
  `runtime_v2_owner_local_waiter_static_test.go` 53.

## Epic 3 Tasks 09-14 Handoff

- Scope completed: proved channel, task, scope, blocking, timer, select, and
  cancellation waiter users after the owner-local waiter-store move.
- Added pending channel/timer contracts in
  `internal/vm/runtime_v2_waiter_contract_test.go`:
  `TestRuntimeV2ChannelCloseWakesSendWaiters` and
  `TestRuntimeV2CancelledSelectCleansWaitKeysAndTimers`.
- Added pending task/scope/blocking contracts in
  `internal/vm/runtime_v2_task_scope_blocking_waiter_contract_test.go`:
  cancelled join waiter cleanup, failfast scope owner wake, blocking completion
  wake, and cancelled blocking waiter cleanup.
- Tasks 10, 12, and 14 closed as no-op runtime migrations. Task 08 had already
  moved waiter storage to `rt_shard.waiter_store`; the affected users call
  compatibility helpers that now route through `rt_executor_waiter_store()`.
- Direct legacy waiter-field audit remains clean: no `->waiters`,
  `->waiters_len`, `->waiters_cap`, or `->net_waiters_len` uses in
  `runtime/native` or `internal/vm`.
- Passing probes recorded in `03-evidence.md`: full pending waiter contract set,
  channel MT probes, `TestMTCorrectnessChannels`, wakeup/structured/blocking MT
  probes, default waiter static proof, and owner-local pending static proof.
- Known debt recorded: `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timeout after
  30s, including isolated reruns. `TestMTBlockingChannelHelpersAllowTimersToAdvance`
  passed and remains the stable Runtime V2 gate member.
- No runtime/native files changed in Tasks 09-14. New tests are pending local
  proofs until Task 18 decides what to promote into CI.
- Closeout gates passed for Tasks 09-14: default waiter static proof, full
  pending waiter contract set, channel MT probes, task/scope/blocking MT probes,
  `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check`.
- Sentrux batch scans: root `6206`, runtime `5220`, runtime/native `5184`.
  Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.

## Epic 3 Tasks 15-16 Handoff

- Scope completed: proved the current net waiter trace contract and migrated
  net waiter traversal/completion behind owner-local waiter helper APIs.
- Added pending proof
  `internal/vm/runtime_v2_net_waiter_contract_test.go`:
  `TestRuntimeV2NetWaiterTraceContract`.
- The pending proof runs a small LLVM net server, drives repeated TCP
  request/reply traffic, sends `SIGUSR1`, and validates live plus exit
  `TRACE_NET` lines.
- Trace contract now checks field presence, nonzero net poll/readiness/direct
  wait/rebuild/complete counters, `io_poll_rebuilds == io_poll_calls`, and
  `io_waiter_net_entries <= io_waiter_scan_entries`.
- Added owner-local helper API:
  `rt_executor_waiter_len`, `rt_executor_net_waiter_len`,
  `rt_executor_visit_net_waiters`, and
  `rt_executor_wake_net_waiters_for_key`.
- The wake helper is explicitly net-only and rejects non-net keys. Generic
  channel/task/scope wake policy remains outside this task.
- `rt_net.c` still owns fd dedupe, `poll()`, wake-fd drain, and trace counters.
  The task did not introduce a persistent fd registry, accept ownership,
  wake-fd relocation, scheduler changes, `N>1`, `eventfd`, `epoll`, `kqueue`,
  or `io_uring`.
- Line counts after Task 16: `rt_net.c` 1024, `rt_async_waiter.c` 309,
  `rt_async_internal.h` 483, `runtime_v2_net_waiter_contract_test.go` 249,
  and `runtime_v2_waiter_static_test.go` 90.
- Checks passed: pending net trace contract, `TestMTNetWaiterWakeupLatency`,
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer`, default waiter
  static boundary, `make c-check`, `make cppcheck`, `make runtime-v2-check`,
  `make check`, and `git diff --check`.
- Read-only review subagent found no P0/P1 blockers. The only P2 was that the
  new pending test file was untracked before staging; close this in the
  Task 15-16 commit scope.
- Native net benchmark before/after ran with freshly built current-checkout
  binaries and wrote ignored reports:
  `build/benchmarks/runtime-v2-epic3-task16-native-net-before.md` and
  `build/benchmarks/runtime-v2-epic3-task16-native-net-after.md`.
- Benchmark trace rows stayed comparable. For the first `1 echo seq` row,
  before had `poll calls=4673`, `poll rebuilds=4673`, `poll allocs=2`; after
  had `poll calls=4421`, `poll rebuilds=4421`, `poll allocs=2`.
- Sentrux native session: 5184 -> 5178, `pass=true`, no violations. Post scans:
  root `6203`, runtime `5214`, runtime/native `5178`.
- Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Known debt: `rt_net.c` remains over the 500 LOC target at 1024 lines. Task 17
  owns the next large-file refactor tranche.
- Known future work: net close/cancel/fd-registry lifecycle proof remains out of
  scope until the fd registry epic. Task 18 owns CI promotion for pending net
  proofs.

## Epic 3 Task 17 Handoff

- Scope completed: extracted trace and SIGUSR1 dump responsibility from
  `runtime/native/rt_async_state.c` into
  `runtime/native/rt_async_trace.c`.
- The new module owns `TRACE_EXEC`, `TRACE_EXEC_SNAPSHOT`, `SCHED_TRACE`, trace
  buffers, trace init/dump, signal-dump request handling, and trace counters.
- Scheduler trace source mapping now uses the explicit
  `rt_trace_sched_source` enum instead of raw `0`/`1`/`2` values.
- No scheduler, waiter, timer, channel, or net behavior was changed. No dead
  code was deleted.
- Line counts after Task 17: `rt_async_state.c` 1731,
  `rt_async_trace.c` 497, `rt_async_internal.h` 499, and `rt_net.c` 1024.
- Checks passed after the refactor: stable MT trace subset,
  `TestMTNetWaiterWakeupLatency`, pending
  `TestRuntimeV2NetWaiterTraceContract`, `git diff --check`, `make c-check`,
  `make cppcheck`, `make runtime-v2-check`, and `make check`.
- Read-only review subagent found no blockers. Its only advisory was to include
  the new `rt_async_trace.c` file in the commit.
- Sentrux native session: 5178 -> 5218, `pass=true`, no violations. Post scans:
  root `6208`, runtime `5255`, runtime/native `5218`.
- `check_rules` still reports missing `.sentrux/rules.toml`; this remains debt,
  not rule compliance.

## Epic 3 Task 18 Handoff

- Scope completed: added stable waiter liveness checks to the Runtime V2 local
  and CI gate path.
- Added `runtime-v2-waiter-check` as a companion Makefile target. It runs the
  default-tag `TestRuntimeV2WaiterHelperStaticBoundary` proof and the exact
  `runtime_v2_pending` waiter proof set promoted from Tasks 04, 07, 09, 11, 13,
  and 15.
- `make runtime-v2-check` still runs the existing LLVM MT seed first, then calls
  `make runtime-v2-waiter-check`.
- `.github/workflows/ci.yml` was unchanged. The `Runtime V2 liveness (llvm)` job
  already installs LLVM and invokes `make runtime-v2-check`, so CI now reaches
  the waiter gate through the same entrypoint.
- Excluded from the green gate: broad accepted-debt regex
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`,
  `TestMTBlockingChannelHelpersDoNotParkWorkers`, and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`.
- Checks passed: `make runtime-v2-waiter-check`, `make runtime-v2-check`,
  `make check`, and `git diff --check`.
- Sentrux root session: 6198 -> 6198, `pass=true`, no violations. Post scans:
  root `6198`, runtime `5195`, runtime/native `5159`.
- `check_rules` still reports missing `.sentrux/rules.toml`; this remains debt,
  not rule compliance.

## Epic 3 Task 19 Handoff

- Scope completed: structural closeout for Epic 3. The durable epic document,
  Runtime V2 epic README, task index, notes, and evidence ledger now mark Epic
  3 complete and preserve the handoff to Epic 4.
- Current closeout claim is local and bounded. Do not state CI green unless a
  fresh CI run is recorded. The existing CI workflow reaches
  `make runtime-v2-check`, and Task 18 made that target run the stable waiter
  liveness gate.
- Main-session closeout gates passed: `make runtime-v2-check`, `make cppcheck`,
  `git diff --check`, `make c-check`, and `make check`.
- `make runtime-v2-check` ran the existing MT seed and
  `runtime-v2-waiter-check`; the waiter set included
  `TestRuntimeV2WaiterHelperStaticBoundary` and all promoted
  `runtime_v2_pending` waiter proofs, including
  `TestRuntimeV2NetWaiterTraceContract`.
- Fresh net and channel benchmarks passed with
  `/tmp/surge-epic3-closeout.Oo0179/surge`, built from `c9fb2f8e`. The reports
  were written under ignored `build/benchmarks/` paths and must not be added.
- Net first row: `1 echo seq`, `60.08 us/op`, `net direct waits=1787`,
  `net poll calls=4028`, `net ready=1787`, `waiter scan entries=12080`,
  `net waiter entries=4028`, `poll rebuilds=4028`, `poll allocs=2`,
  `complete calls=3574`, `completed waiters=1787`.
- Channel key rows: `1 channel_reused_reply` at `3289 ns/op` with handoff
  yields `19999` and fallback fields `0`; `1 channel_sync_new_reply` at
  `9150 ns/op` with task-context blocking sends `5000` and recvs `5000`.
- Post-doc Sentrux closeout scans: root `6198`, runtime `5195`,
  runtime/native `5159`. `check_rules` still reports missing
  `.sentrux/rules.toml` for all three paths; this remains debt, not rule
  compliance.
- Remaining debt to keep named: broad focused VM regex, missing Sentrux rules,
  timeout-sensitive tests `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`,
  over-500-line `rt_async_state.c` and `rt_net.c`, and no persistent fd registry
  in Epic 3.
- Epic 4 should start with persistent fd registry and net lifecycle proof:
  registration, readiness lifetime, close/cancel cleanup, wake-fd ownership,
  and shutdown behavior. Do not start Epic 4 with `N>1` or crossing syntax.

## Epic 4 Task 9 Handoff

- Scope completed: close, cancellation/re-register stale completion, and
  stale poll snapshot protection are now fd-registry lifecycle concerns.
- fd rows use registry-owned monotonic generations; close snapshots carry
  fd/generation plus exact accept/read/write interests.
- close marks rows closed under `ex->lock`, raw-closes outside the executor
  lock, then wakes only the snapshot keys and signals net poll/`io_cv`.
- poll snapshots exclude closed rows and carry generation; poll-error and
  readiness completion go through registry guarded completion helpers.
- Task 8 close tests are no longer expected-red:
  `CloseWakesParkedAcceptWaiter` and `CloseWakesParkedReadWaiter` both pass.
- Deterministic stale proof uses fd `42` in a tiny C registry test; no OS fd
  allocation luck is involved.
- Boundary recorded as RV2-DEBT-010: copied public net handles still carry the
  raw fd view and are not generation-aware yet.
- Trace zero contract stays intact: `io_waiter_scan_entries`,
  `io_waiter_net_entries`, and `io_poll_dedup_checks` remain asserted zero by
  `TestRuntimeV2NetWaiterTraceContract`.
- Checks passed: static fd-registry proof, focused Task 8 lifecycle quartet,
  `TestRuntimeV2NetWaiterTraceContract`, `TestMTNetWaiterWakeupLatency`,
  `make c-check`, `make cppcheck`, `make runtime-v2-check` after one isolated
  `TestMTChannelParkUnpark` timeout/rerun, `make check`, and
  `git diff --check`.

## Epic 4 Task 10 Handoff

- Scope completed: wake-fd and shutdown tests only. No runtime C, Makefile,
  CI, `STATS.md`, or `DEBT.md` changes.
- LOC discipline: new Task 10 tests live in separate opt-in files
  `runtime_v2_fd_registry_wake_test.go` (`446` lines) and
  `runtime_v2_fd_registry_shutdown_static_test.go` (`133` lines); existing
  contract/static files remain `499` and `426` lines.
- Green runtime trace proof:
  `TestRuntimeV2FDRegistryWakeFDObservedForInterestAddedDuringPoll` asserts
  `io_poll_wake_fd>=1`, `io_poll_waiters_max>=2`, and zero legacy poll-build
  counters on live `SIGUSR1` and exit traces.
- Green close proof:
  `TestRuntimeV2FDRegistryCloseWakePollNotificationProof` is a deterministic C
  behavior check around `rt_fd_registry_wake_closed_net_waiters`; it proves
  current Task 9 code calls both `rt_net_wake_poll()` and
  `pthread_cond_broadcast(&ex->io_cv)` when a close snapshot wakes waiters.
  Close wake-fd behavior is not expected-red anymore.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryCancelledInterestWakesPoller` fails only because
  `io_poll_wake_fd` stays `2 -> 2` after cancellation-side interest removal.
  The test uses a dedicated stderr pipe/scanner and waits for the
  `reason=sigusr1` baseline before releasing the gate; the baseline already
  has two parked fd rows and the legacy scan counters are zero.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryShutdownDrainStaticContract` fails only because
  `rt_executor_request_shutdown` and
  `rt_executor_drain_shutdown_net_waiters` are not declared. The names are the
  current explicit-status shutdown contract proposal, following the
  owner-first `rt_executor_*` helper style.
- Important testing note: a runtime close `SIGUSR1` delta test was rejected
  during implementation because `reason=sigusr1` dumps are drained
  asynchronously and can be emitted after the gate release. Keep close wake
  coverage as the direct C behavior proof unless Task 11 adds a synchronous
  trace hook.
- Checks run: green wake/close proof command passed; cancellation expected-red
  command failed with the intended `io_poll_wake_fd` delta assertion; shutdown
  static command failed with the intended two undeclared identifiers;
  `TestMTNetWaiterWakeupLatency` passed; `gofmt -l` and `git diff --check`
  were clean.

## Epic 4 Task 11 Handoff

- Scope completed: wake-fd and shutdown migration focused checks, heavy gates,
  Sentrux, and read-only review subagent passed.
- Cancellation-side net waiter removal now notifies the poller only after the
  last same-key open net interest is detached from the fd registry. This is
  the explicit "in-flight poll snapshot may be stale" signal. Readiness
  completion and `pop_waiter` do not write extra wake bytes.
- Shutdown drain behavior is registry-owned:
  `rt_fd_registry_drain_shutdown_net_waiters_locked` snapshots registry rows,
  wakes exact accept/read/write keys through
  `rt_executor_wake_net_waiters_for_key`, clears matching fd-lifetime rows,
  and wakes poll/`io_cv` only when it drained interests.
- Public owner/control-plane wrappers live in new
  `runtime/native/rt_shutdown.c`: `rt_executor_drain_shutdown_net_waiters`
  and `rt_executor_request_shutdown`. The request API is not wired into normal
  program lifecycle in Task 11.
- LOC discipline: `rt_async_state.c` was not modified and stayed `1727`
  lines; `rt_async_internal.h` stayed below limit at `495` lines; new
  `rt_shutdown.c` is `33` lines.
- Added deterministic proof
  `TestRuntimeV2FDRegistryShutdownDrainBehavior`: public drain wrapper wakes
  registered net keys, clears registry rows/interests, notifies wake-poll/io-cv
  on non-empty drain, and leaves empty drain as OK no-op.
- Former expected-red Task 10 checks are now green:
  `TestRuntimeV2FDRegistryCancelledInterestWakesPoller` and
  `TestRuntimeV2FDRegistryShutdownDrainStaticContract`.
- Focused checks passed: shutdown static+behavior tests, cancellation
  wake-fd test, Task 10 wake/close green tests, `TestMTNetWaiterWakeupLatency`,
  `gofmt -l`, `git diff --check`, `make c-check`, and `make cppcheck`.
- Heavy gates passed: `make runtime-v2-check`, `make check`, and Sentrux
  root/runtime/runtime-native gates (`6198 -> 6194`, `5195 -> 5239`,
  `5159 -> 5184`).
- Review subagent returned APPROVE with no P0/P1/P2 findings. Residual
  boundary: `rt_executor_request_shutdown` is not wired into normal lifecycle
  yet, and blocking-worker shutdown behavior is not behavior-tested in Task 11.
