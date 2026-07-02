# Epic 4 Evidence

This file is the task evidence ledger for Epic 4, persistent fd registry and
net lifecycle ownership. Keep entries short, exact, and command-backed.

## Starting Evidence

- Epic 3 is complete for owner-local waiters and dependency-aware runtime
  refactoring under `N=1`.
- Persistent fd registry, net lifecycle ownership, accept distribution, `N>1`,
  crossing syntax, and backend I/O changes were not implemented in Epic 3.
- Current net polling still builds temporary fd rows from net waiter state in
  `runtime/native/rt_net.c`.
- Current known line-count debt from Epic 3 closeout:
  - `runtime/native/rt_async_state.c`: 1731 lines;
  - `runtime/native/rt_net.c`: 1024 lines;
  - `runtime/native/rt_async_trace.c`: 497 lines;
  - `runtime/native/rt_async_internal.h`: 499 lines.
- The broad focused VM command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted
  backend-test debt and is not an Epic 4 green gate.
- Sentrux rule files were added during pre-Epic 4 quality hardening for root,
  `runtime/`, and `runtime/native`.

## Task Evidence Ledger

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Kickoff baseline, Sentrux state, line counts, and gate plan recorded below. |
| 2 | Pending | FD registry dependency map. |
| 3 | Pending | FD lifecycle behavior contract tests. |
| 4 | Pending | Registry static shape tests. |
| 5 | Pending | Registry container skeleton. |
| 6 | Pending | Net wait registration migration. |
| 7 | Pending | Poll-from-registry migration. |
| 8 | Pending | Close/cancel/re-register behavior tests. |
| 9 | Pending | Close/cancel/re-register migration. |
| 10 | Pending | Wake-fd and shutdown behavior tests. |
| 11 | Pending | Wake-fd and shutdown migration. |
| 12 | Pending | Trace counters and benchmark contract. |
| 13 | Pending | CI gate wiring. |
| 14 | Pending | Large-file refactor tranche. |
| 15 | Pending | Closeout gates and handoff. |

## Task 1 Evidence: Kickoff Baseline And Sentrux

Date: 2026-07-02. Docs-only task; no runtime, compiler, test, Makefile, CI, or
Sentrux rule changes.

Checkout state:

- Branch: `codex/runtime-net-scheduler-refactor`.
- Baseline commit: `05ceb7c20b19e72125e320f07445959cb2b349bf`
  (`chore(runtime): enforce Runtime V2 quality gates`).
- `git status --short` was empty before the task.
- `git diff --check` passed with empty output.

Runtime/native line counts at kickoff:

- `runtime/native/rt_net.c`: 1024 (over limit; allowlisted at 1024);
- `runtime/native/rt_async_state.c`: 1731 (over limit; allowlisted at 1731);
- `runtime/native/rt_async_trace.c`: 497;
- `runtime/native/rt_async_internal.h`: 499;
- `runtime/native/rt_async_waiter.c`: 309;
- `runtime/native/rt_runtime.c`: 161.

Sentrux state (CLI; the Sentrux MCP server is not connected in this session,
so `session_start`/`session_end` are replaced by `sentrux gate --save` CLI
baselines; this is recorded as tool-availability state, not rule
non-compliance):

- `sentrux check .`: passed, 10 rules, quality `6198`;
- `sentrux check runtime`: passed, 7 rules, quality `5195`;
- `sentrux check runtime/native`: passed, 7 rules, quality `5159`;
- `sentrux gate --save` recorded baselines `6198` / `5195` / `5159` for the
  three mandatory scan roots.

Startup gates:

- `make c-check`: passed;
- `make cppcheck`: passed (31/31 files);
- `make runtime-v2-check`: first run failed once inside the MT seed set
  (`FAIL surge/internal/vm 38.558s`); a full isolated rerun passed with
  `exit=0` (seed `7.801s`, waiter static `0.033s`, pending waiter set
  `19.130s`). This matches the flake class already recorded in Epic 3 Task 06
  (`TestMTBlockingChannelHelpersAllowTimersToAdvance` internal program
  timeout under load). Recorded as pre-existing flake debt, not a new Epic 4
  regression.
- `make check`: passed, including `check_file_sizes.sh` with the
  `.loc-legacy-allowlist` ceilings.

Accepted debt carried into Epic 4 (unchanged):

- broad focused VM command `go test ./internal/vm -run 'MT|Async|Net|LLVM'`
  stays accepted backend-test debt (RV2-DEBT-001) and is not an Epic 4 gate;
- `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` remain
  excluded from green gates (RV2-DEBT-002);
- `rt_async_state.c` and `rt_net.c` remain over the 500-line target
  (RV2-DEBT-003, RV2-DEBT-004; Task 14 owns the Epic 4 tranche).

Gate plan for Tasks 2-7:

- Task 2 (docs map): `git diff --check` plus targeted `rg` symbol evidence;
  no runtime tests required.
- Task 3 (contract tests): default tag-off proof for the new test file,
  tagged pending proof where tests describe future registry behavior, and
  the focused net probe
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s`.
- Task 4 (static tests): exact
  `go test ./internal/vm -run TestRuntimeV2FDRegistryStatic -v --timeout 90s`
  in its approved tag mode plus `git diff --check`.
- Tasks 5-7 (runtime code): `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, Task 3/4 proofs in their approved
  modes, focused net wake probe `TestMTNetWaiterWakeupLatency`, native net
  benchmark with a current-checkout `SURGE` binary and outer `timeout` for
  Task 7, `git diff --check`, and Sentrux root plus scoped CLI checks with
  the saved gate baselines.

## Draft Creation Evidence

- `git diff --check`: passed with empty output after creating the Epic 4
  document set.
- Sentrux repository scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6198`, `files=4775`, `import_edges=1890`, and
  `lines=377913`.
- Sentrux rules were added after draft creation. Root `sentrux check .` passes
  with quality `6198`.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5195`, `files=35`, `import_edges=33`, and `lines=15275`.
- Runtime `sentrux check runtime` passes with quality `5195`.
- Sentrux runtime/native scan for
  `/home/zov/projects/surge/surge/runtime/native` returned
  `quality_signal=5159`, `files=34`, `import_edges=33`, and `lines=15260`.
- Runtime/native `sentrux check runtime/native` passes with quality `5159`.
- MCP `check_rules` also passes for root, `runtime/`, and `runtime/native`.
