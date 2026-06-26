# Epic 3 Task 15: Net Waiter Tests And Trace Contract

**Goal:** prove net waiter liveness and trace behavior before net waiter
migration.

**Approach:** add focused tests or probes for net read/write/accept waiters
while preserving the current poll-set rebuild model.

**Skills:** `task-breakdown`, `static-analysis`,
`writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_net.c`, `LIVENESS_PROBES.md`,
`./scripts/bench_native_net.sh`

---

## Files

- Test or probe: focused net waiter tests under `internal/vm/` or scripts.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Prove net waiter wakeup without adding a persistent fd registry.
2. Record trace counters that must stay comparable.
3. Run net liveness probes and native net benchmark baseline if the path is
   performance-sensitive.

## Done

- Task 16 has net-specific behavior and trace proof.
