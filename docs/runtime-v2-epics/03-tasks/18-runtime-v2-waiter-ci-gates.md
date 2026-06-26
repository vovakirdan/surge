# Epic 3 Task 18: Runtime V2 Waiter CI Gates

**Goal:** add stable waiter liveness checks to local and CI gates.

**Approach:** promote only tests proven stable in earlier tasks. Do not add the
broad accepted-debt VM regex.

**Skills:** `ci-cd-best-practices`, `writing-clearly-and-concisely`

**Tech Details:** `Makefile`, `.github/workflows/`, `make runtime-v2-check`

---

## Files

- Modify: `Makefile`
- Modify: `.github/workflows/*` if needed.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Select stable waiter tests from Tasks 04, 09, 11, 13, and 15.
2. Add them to the local Runtime V2 gate or a named companion target.
3. Wire CI to the same stable command.
4. Record why any known waiter probe is not yet in CI.

## Done

- CI can catch stable waiter regressions.
- Accepted backend-test debt remains out of the green gate.
