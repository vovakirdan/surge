# Epic 2 Task 10: Channel/Blocking Compatibility Tests

**Goal:** prove channel and blocking fallback state can move without changing
direct async channel behavior or sync helper compensation.

**Approach:** run and extend channel tests before moving counters or fallback
integration. Add only tests that prove current behavior; do not turn sync
fallback into the V2 hot path.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `internal/vm/mt_executor_test.go`,
`scripts/bench_native_channels.sh`, `SURGE_TRACE_EXEC=1`

---

## Files

- Test: `internal/vm/mt_executor_test.go`
- Script evidence: `scripts/bench_native_channels.sh`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Run direct async channel wakeup tests from `LIVENESS_PROBES.md`.
2. Run `TestMTBlockingChannelHelpers...` tests with
   `SURGE_SKIP_TIMEOUT_TESTS=0`.
3. Run native channel benchmark with an outer timeout.
4. Record direct channel counters and sync fallback counters separately.
5. Add a focused test if moved state would otherwise have no regression guard.
6. Decide which test subset joins `make runtime-v2-check`.

## Done

- Direct async channel and sync fallback evidence are separate.
- Any new test has a CI owner.
- The task does not normalize sync fallback as the target V2 hot path.
