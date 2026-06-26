# Epic 3 Task 19: Epic Closeout And Static Gates

**Goal:** close Epic 3 with consolidated evidence and a clean handoff to Epic 4.

**Approach:** run full local gates, Sentrux scans, focused liveness probes, and
benchmarks for touched performance paths. Move durable notes into the owning
docs.

**Skills:** `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `make runtime-v2-check`, `make check`, `make c-check`,
`make cppcheck`, Sentrux, native benchmarks

---

## Files

- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/03-owner-local-waiters-and-runtime-refactor.md`
- Modify: `docs/runtime-v2-epics/README.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Verify every Epic 3 task has evidence and notes.
2. Run full required gates or record blockers.
3. Rerun root and scoped Sentrux scans.
4. Consolidate durable decisions from notes.
5. State exact Epic 4 starting point and remaining debt.

## Done

- Epic 3 is complete or explicitly blocked.
- The next epic can start without relying on chat context.
