# Epic 3 Evidence

This file records task evidence for
`03-owner-local-waiters-and-runtime-refactor.md`.

## Starting State

Epic 3 is drafted but not implemented.

Known starting facts:

- Epic 2 completed the `N=1` `rt_runtime` / `rt_shard` skeleton.
- Waiters still use executor-global storage.
- The broad VM/native/LLVM regex remains accepted backend-test debt.
- Missing Sentrux rules remain debt, not compliance.
- The largest relevant native files at draft time are:
  - `runtime/native/rt_async_state.c`: 2431 lines;
  - `runtime/native/rt_term.c`: 1091 lines;
  - `runtime/native/rt_net.c`: 1040 lines;
  - `runtime/native/rt_fs.c`: 978 lines;
  - `runtime/native/rt_async_task.c`: 768 lines;
  - `runtime/native/rt_async_channel.c`: 549 lines;
  - `runtime/native/rt_async_internal.h`: 460 lines.
- Initial dead-code seed: `rt_select_poll_tasks` is suspect only and must not
  be removed without generated-IR search, ABI review, focused tests, and
  Sentrux evidence.

## Task Evidence Ledger

Add one section per closed task. Use `EVIDENCE_TEMPLATE.md` for runtime-code
tasks and record exact commands, Sentrux paths, line-count changes, and known
debt.

## Task 01: Kickoff Baseline And Sentrux

Status: complete.

Branch and commit:

- Branch: `codex/runtime-net-scheduler-refactor`, ahead of origin by one commit
  at task start.
- Start commit: `f4f83c4d docs(runtime): draft Runtime V2 waiter epic`.
- Working tree: clean before implementation.

Runtime/native line-count baseline:

- `runtime/native/rt_async_state.c`: 2431 lines.
- `runtime/native/rt_term.c`: 1091 lines.
- `runtime/native/rt_net.c`: 1040 lines.
- `runtime/native/rt_fs.c`: 978 lines.
- `runtime/native/rt_async_task.c`: 768 lines.
- `runtime/native/rt_string.c`: 762 lines.
- `runtime/native/rt_bignum_int.c`: 744 lines.
- `runtime/native/rt_bignum_uint_div.c`: 718 lines.
- `runtime/native/rt_bignum_float_core.c`: 654 lines.
- `runtime/native/rt_bignum_api.c`: 640 lines.
- `runtime/native/rt_async_channel.c`: 549 lines.
- `runtime/native/rt_bignum_format.c`: 501 lines.
- `runtime/native/rt_async_internal.h`: 460 lines.

Startup gates:

- `make runtime-v2-check`: passed.
- `make c-check`: passed.
- `make cppcheck`: passed.
- `make check`: passed.

Sentrux baseline:

- Root scan `/home/zov/projects/surge/surge`: `quality_signal=6207`,
  bottleneck `modularity`.
- Runtime scan `/home/zov/projects/surge/surge/runtime`:
  `quality_signal=5209`, bottleneck `redundancy`.
- Native scan `/home/zov/projects/surge/surge/runtime/native`:
  `quality_signal=5172`, bottleneck `redundancy`.
- `session_start` saved the native scan baseline at `quality_signal=5172`.
- `check_rules`: missing `.sentrux/rules.toml` for root, runtime, and
  runtime/native. This remains debt, not compliance.

Accepted debt confirmed:

- Do not use `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required
  green gate in Epic 3.
- Missing Sentrux rules are recorded honestly and cannot be treated as a
  passing rule gate.

## Task 02: Waiter Dependency Map

Status: complete.

Output:

- Created `docs/runtime-v2-epics/03-waiter-dependency-map.md`.

Scope:

- Read-only map of current waiter keys, storage, call graph, cleanup paths,
  contract/detail/open-decision split, and later probes.
- No runtime/native code changed.
- No tests were required for this read-only mapping task.

Key findings:

- Current waiter storage is executor-global under `ex->lock`.
- `wake_token` is the current wake-before-park guard.
- `net_waiters_len` is a polling hint and must not become a hidden fd registry.
- Shutdown-adjacent waiter cleanup lacks a scoped contract.
- FIFO-by-key must be decided before owner-local storage changes.

## Task 03: Refactor Dependency And Dead-Code Audit

Status: complete.

Output:

- Created `docs/runtime-v2-epics/03-refactor-audit.md`.

Scope:

- Read-only audit of runtime file pressure, waiter-related dependency clusters,
  first safe extraction tranche, tranche order, and dead-code suspects.
- No runtime/native code changed.
- No code deletion was authorized.

Key findings:

- First safe refactor tranche is legacy waiter key/list extraction into a
  cohesive waiter module, with storage still executor-global.
- `wake_task_with_policy`, `wake_key_all_with_policy`, `park_current`,
  `clear_select_timers`, net polling, channel handoff, and task/select ABI must
  stay in place for the first extraction.
- No proven-dead code was found.
- `rt_select_poll_tasks` remains suspect-only because it has native, public ABI,
  and LLVM builtin references.

## Task 04: Waiter Behavior Contract Tests

Status: complete as a pending red proof.

Output:

- Added `internal/vm/runtime_v2_waiter_contract_test.go` under the
  `runtime_v2_pending` build tag.

Covered contracts:

- A cancelled recv waiter must not consume the next channel wake.
- A cancelled send waiter must not consume the next receiver wake.
- Channel close must wake all recv waiters with `nothing`.
- A select timeout must clean the losing channel waiter before the next recv.

Default-gate safety:

```bash
go test ./internal/vm \
  -run '^TestRuntimeV2(CancelledRecvWaiterDoesNotConsumeNextWake|CancelledSendWaiterDoesNotConsumeNextRecv|ChannelCloseWakesRecvWaiters|SelectTimeoutCleansLosingChannelWaiter)$' \
  -count=1 -parallel=1 -p=1 --timeout 30s
```

Result: passed with no tests selected because `runtime_v2_pending` is off by
default.

Pending proof:

```bash
SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm \
  -run '^TestRuntimeV2(CancelledRecvWaiterDoesNotConsumeNextWake|CancelledSendWaiterDoesNotConsumeNextRecv|ChannelCloseWakesRecvWaiters|SelectTimeoutCleansLosingChannelWaiter)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 60s
```

Result: failed. All four generated programs print `ok` and then exit with
`exit=-1`. This means the user-visible waiter scenario reaches the expected
result, then runtime cleanup or stale wake/re-poll behavior crashes afterward.

Focused crash evidence for
`TestRuntimeV2CancelledRecvWaiterDoesNotConsumeNextWake`:

```bash
SURGE_STDLIB=/home/zov/projects/surge/surge go run ./cmd/surge build \
  target/debug/.tests/TestRuntimeV2CancelledRecvWaiterDoesNotConsumeNextWake/TestRuntimeV2CancelledRecvWaiterDoesNotConsumeNextWake.sg \
  --emit-mir --emit-llvm --keep-tmp
gdb -q -batch -ex 'set env SURGE_THREADS 2' -ex 'run' -ex 'bt' \
  --args ./target/debug/TestRuntimeV2CancelledRecvWaiterDoesNotConsumeNextWake
```

Backtrace root:

```text
Thread 3 "TestRuntimeV2Ca" received signal SIGSEGV, Segmentation fault.
#0  rt_string_len_bytes ()
#1  fn ()
#2  fn ()
#3  __surge_poll_call ()
#4  poll_user_task ()
#5  poll_task ()
#6  rt_worker_main ()
```

Debt carried forward:

- These tests are not a default CI gate yet.
- The implementation tasks must make the pending waiter contract pass before
  promotion into `runtime-v2-check`.
- Net waiter close/cancel/readiness behavior and shutdown liveness still need
  their own tests in Tasks 15-16.

## Draft Creation Evidence

- Docs created for Epic 3 scope and brief task list.
- `git diff --check`: passed.
- Sentrux root scan: `/home/zov/projects/surge/surge`, `quality_signal=6207`.
- Sentrux runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5209`.
- `check_rules`: missing `.sentrux/rules.toml` for both scanned paths. This is
  debt, not rule compliance.
