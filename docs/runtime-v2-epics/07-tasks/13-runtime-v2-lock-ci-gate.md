# Epic 7 Task 13: Runtime V2 Lock CI Gate

**Kind:** CI/build wiring. **Depends on:** Tasks 4, 5, 11.

**Goal:** promote the lock-split contract to a stable gate:
`make runtime-v2-lock-check` runs the eight static shape gates and the
nine cross-shard behavior modes, and `make runtime-v2-check` (already run
by the `runtime-v2-check` CI job) invokes it.

## Scope

- New Makefile target `runtime-v2-lock-check` (added to `.PHONY`):
  - static shape gates (120s timeout): LaneAPIShape, ShardSyncShape,
    WorkerLoopShardLane, NoAmbiguousGlobalLock, ClockAndSleepStoreShape,
    NoWholeTableSleepScan, ChannelOwnerShape, GlobalCondvarRetirement;
  - behavior modes (300s timeout, `SURGE_BACKEND=llvm`, `-count=1
    -parallel=1 -p=1`): CrossShardJoin, CrossShardCancel,
    CrossShardChannelFifoAndClose, ChannelCloseWakesParkedReceiver,
    SelectAcrossOwners, TimeoutAcrossOwners, SleepIdleAdvanceMultiShard,
    BlockingCompletionCrossShard, ShutdownWakesAllLanes (each runs at
    `SURGE_SHARDS=1` and `SURGE_SHARDS=3` inside the test).
- `runtime-v2-check` calls it after `runtime-v2-accept-check`; CI runs
  `make runtime-v2-check` in `.github/workflows/ci.yml` (same promotion
  path as the Epic 6 accept gate — no separate workflow job).
- The `runtime_v2_pending` build tag stays on the test files: the tag is
  how the gate opts them in explicitly while `go test ./...` skips them.

## Checks

`make runtime-v2-lock-check` green standalone; `make runtime-v2-check`
green with the new stage; `make check` pre-commit.

## Success Criteria

Lock gate runs in `runtime-v2-check` and therefore CI; own commit.
