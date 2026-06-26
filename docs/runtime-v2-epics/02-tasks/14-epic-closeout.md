# Epic 2 Task 14: Epic Closeout

**Goal:** close Epic 2 with durable evidence, current notes, and a clean handoff
to the owner-local waiter epic.

**Approach:** consolidate task evidence into the epic document and notes. Verify
that no hidden blocker, skipped test, or stale accepted debt is being treated as
green.

**Skills:** `writing-clearly-and-concisely`, `code-review-expert`

**Tech Details:** `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`,
`docs/runtime-v2-epics/02-evidence.md`, `NOTES.md`, Sentrux

---

## Files

- Modify: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify: `docs/runtime-v2-epics/README.md`
- Modify: later epic draft only if Epic 3 is started.

## Steps

1. Verify every task has an evidence entry and commit.
2. Run final `make check`, `make c-check`, and `make cppcheck`.
3. Run final Runtime V2 CI target locally.
4. Run Sentrux root/runtime scans, `health`, and `check_rules`.
5. Confirm accepted VM debt is still assigned to the later test/backend epic.
6. Confirm CI now covers the stable Runtime V2 tests added in this epic.
7. Confirm no owner-local waiter, fd registry, `N>1`, or crossing syntax work
   slipped into Epic 2.
8. Update notes with the exact starting point for Epic 3.

## Done

- Epic 2 status is complete.
- CI and local evidence are linked.
- Worktree is clean after closeout commit.
- Epic 3 can start without chat context.
