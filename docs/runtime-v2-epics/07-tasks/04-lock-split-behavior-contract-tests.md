# Epic 7 Task 4: Lock-Split Behavior Contract Tests

**Kind:** test writing. **Depends on:** Tasks 1, 2, 3.

**Goal:** pin the observable behavior that must survive the lock split as
focused, timeout-bounded tests before any lane migrates. These tests must
pass on the current global-lock runtime and keep passing after every Task
6-11 commit; they are the regression harness for the whole migration.

## Deliverable

A native C harness embedding the full runtime (`rt_entry.c` excluded), in
the established scheduler-placement-harness pattern, driven by Go tests
tagged `runtime_v2_pending`:

- `internal/vm/runtime_v2_lock_split_harness_test.go`: build helper plus the
  C harness source.
- `internal/vm/runtime_v2_lock_split_behavior_test.go`: the Go test
  functions.

Modes and contracts (each runs at `SURGE_SHARDS=1` with `SURGE_THREADS=2`
and at `SURGE_SHARDS=3`/`SURGE_THREADS=3`; the harness pins task placement
modulo the live shard count):

| Mode | Contract |
| --- | --- |
| `cross-join` | a task pinned to one shard joins a task pinned to another; completion value arrives; main-thread await completes (`done_cv` lane). |
| `cross-cancel` | cancelling a task parked on a channel on another shard wakes it and completes it as cancelled. |
| `cross-channel` | one sender shard, one receiver shard, a channel created on a third: FIFO order for 200 values through a capacity-4 buffer, then close completes the receiver with closed status. |
| `close-wakes` | closing a channel wakes a receiver parked on another shard with closed status. |
| `blocking-completion` | a blocking job submitted by a pinned task completes and wakes the awaiting task with its result (pool -> owner-shard wake lane). |
| `sleep-idle-advance` | a pinned sleeping task fires through the idle virtual-clock advance with no runnable work anywhere. |
| `select-across-owners` | a selector task parks over two channels created on two other shards; a send to the second channel resolves the select to that arm. |
| `timeout-across-owners` | `rt_timeout_poll` over a never-completing task on another shard fires the timeout arm and cancels the target. |
| `shutdown-liveness` | with receivers parked on channels on every shard, `rt_executor_request_shutdown` returns and the process exits cleanly (no thread left parked). |

Every mode uses bounded internal waits (attempt-counted, ~milliseconds per
attempt) and returns nonzero on timeout, so a lost wakeup shows up as a fast
failure, not a hung test.

## Out Of Scope

- No runtime C changes.
- No static shape gates (Task 5).
- No Makefile/CI wiring (Task 13 wires the green set).

## Checks

- The new tests pass against the current runtime:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LockSplit(CrossShardJoin|CrossShardCancel|CrossShardChannelFifoAndClose|ChannelCloseWakesParkedReceiver|BlockingCompletionCrossShard|SleepIdleAdvanceMultiShard|SelectAcrossOwners|TimeoutAcrossOwners|ShutdownWakesAllLanes)$' -count=1 -parallel=1 -p=1 -v --timeout 300s`.
- `git diff --check`; Go files formatted; lint via the pre-commit `make
  check` (tagged files are still vetted by golangci-lint? — record actual
  result in evidence).

## Success Criteria

- All modes green on the pre-split runtime at both shard configurations.
- Test files at or near the <=500-line target each (record counts).
- Evidence ledger and `NOTES.md` updated; index status flipped.
