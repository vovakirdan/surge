# Epic 2 Task 13: Accessor Cleanup And Static Gates

**Goal:** remove ambiguous ownership introduced during the migration and verify
static quality before closeout.

**Approach:** clean only the ownership helpers and call sites touched by Tasks
05, 07, 09, and 11. Do not start a second refactor.

**Skills:** `code-refactoring`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/`, `make c-check`, `make cppcheck`,
Sentrux root/runtime scans

---

## Files

- Modify: touched `runtime/native/*` files only.
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Search for direct field access added during Epic 2.
2. Replace ambiguous access with owner-named helpers or local variables.
3. Remove unused helpers introduced by earlier tasks.
4. Check every new or heavily rewritten C file against the 500-line rule.
5. Run `make c-check`, `make cppcheck`, and `make check`.
6. Run Sentrux root/runtime scans and compare with Task 01 baseline.
7. Record any quality drop as a blocker or accepted follow-up.

## Done

- Runtime/shard ownership reads clearly at call sites.
- No unrelated refactor is included.
- Static gates and Sentrux evidence are recorded.
