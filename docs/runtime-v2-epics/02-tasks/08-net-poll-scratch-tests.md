# Epic 2 Task 08: Net Poll Scratch Tests

**Goal:** prove net scratch movement does not change current readiness and wake
behavior.

**Approach:** use existing net wake and benchmark probes before moving state.
Add tests only for the scratch movement surface; do not start the persistent fd
registry rewrite here.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `internal/vm/mt_correctness_test.go`,
`scripts/bench_native_net.sh`, `SURGE_TRACE_EXEC=1`

---

## Files

- Test: `internal/vm/mt_correctness_test.go`
- Script evidence: `scripts/bench_native_net.sh`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Run `TestMTNetWaiterWakeupLatency` with `SURGE_SKIP_TIMEOUT_TESTS=0`.
2. Run native net benchmark with a current-checkout `SURGE` binary and an outer
   timeout.
3. Record key trace rows:
   - `io_poll_calls`
   - `io_poll_net_ready`
   - waiter scan counters
   - direct wait counters.
4. Decide whether a new test is needed for moved scratch storage.
5. If needed, add the smallest test that fails when scratch state is lost across
   a poll cycle.
6. Update the CI contract only if the probe is stable on current checkout.

## Done

- Net migration has before evidence.
- Persistent fd registry behavior remains out of scope.
- CI owner is recorded for any new stable net test.
