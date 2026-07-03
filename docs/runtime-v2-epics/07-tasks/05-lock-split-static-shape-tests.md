# Epic 7 Task 5: Lock-Split Static Shape Tests

**Kind:** test/static checks. **Depends on:** Tasks 1, 2, 3.

**Goal:** encode the spike's structural decisions (D1-D16) as mechanical
source and compile gates, written before the implementation so Tasks 6-11
migrate toward a fixed, testable shape instead of an interpretation.

## Deliverable

`internal/vm/runtime_v2_lock_split_static_test.go` (tagged
`runtime_v2_pending`), eight gates:

| Gate | Pins |
| --- | --- |
| `TestRuntimeV2LockSplitShardSyncShape` | `rt_shard.lock` (mutex), `rt_shard.worker_cv`, `rt_shard.poller_cv` (condvars), `waiter.owner_hint` (D1, D3, D5). |
| `TestRuntimeV2LockSplitLaneAPIShape` | lane entry points exist: `rt_control_lock/unlock`, `rt_shard_lock/unlock`, `rt_lane_debug_enabled` (D2). |
| `TestRuntimeV2LockSplitClockAndSleepStoreShape` | `ex->now_ms` is atomic; `rt_shard.sleep_store` exists (D7). |
| `TestRuntimeV2LockSplitNoAmbiguousGlobalLock` | `rt_lock(`/`rt_unlock(` are gone from `runtime/native`: every call site names its lane (D2, D16). |
| `TestRuntimeV2LockSplitWorkerLoopShardLane` | `rt_worker_main` takes `rt_shard_lock` and never the control lane on the worker turn (D6). |
| `TestRuntimeV2LockSplitNoWholeTableSleepScan` | `tick_virtual` and `advance_time_to_next_timer` no longer reference `tasks_cap` (D7). |
| `TestRuntimeV2LockSplitChannelOwnerShape` | `rt_channel` records `owner_shard_id`; `rt_channel_send_inner` locks the owner shard, never the control lane (D5, channel slice). |
| `TestRuntimeV2LockSplitGlobalCondvarRetirement` | `ready_cv` and `io_cv` disappear from `runtime/native` (D1, D15). |

The file adds a definition-aware function-body finder
(`lockSplitFunctionDefinitionBody`) because the shared `cFunctionBody`
helper matches forward declarations (e.g. `rt_worker_main`'s prototype).

## Expected State

All eight gates are red at the baseline commit by design: they describe the
post-split shape. They are not wired into any Makefile target; `make check`
ignores `runtime_v2_pending` files, and `runtime-v2-check` runs explicit
test lists that do not include them. Task 13 wires them into
`runtime-v2-lock-check` once Tasks 6-11 turn them green. The migration tasks
flip them in this expected order: Task 6 turns the shard-sync and lane-API
gates green; Task 9 the sleep-scan and clock gates; Task 11 (the peel) the
worker-loop, channel, no-ambiguous-lock, and condvar-retirement gates.
Task 7 replaces the global `ready_cv` with per-shard worker condvars and
Task 10 adds channel owner metadata and key routing, but both keep their
paths on the control lane under the sanctioned nested shape: task-state
fields must switch guardians atomically across all their accessors, so
every "no control lane here" gate flips together at the peel.

## Out Of Scope

- No runtime C changes.
- No behavior tests (Task 4).
- No Makefile/CI wiring (Task 13).

## Checks

- `gofmt -l` clean; `go vet -tags runtime_v2_pending ./internal/vm` clean.
- One full run of the eight gates recording the expected-red state with
  clear failure messages (no helper panics or false matches).
- `git diff --check`.

## Success Criteria

- Every D1-D16 structural decision that can be pinned mechanically has a
  gate, and each gate names the decision it pins.
- All gates fail with actionable messages at the baseline.
- Evidence ledger, `NOTES.md`, and the task index are updated.
