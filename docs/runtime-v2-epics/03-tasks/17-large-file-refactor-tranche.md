# Epic 3 Task 17: Large-File Refactor Tranche

**Goal:** reduce the largest touched runtime files after waiter migration
exposes stable module boundaries.

**Approach:** execute the first safe tranche from Task 03. This is a
behavior-preserving refactor, not a feature task.

**Skills:** `code-refactoring`, `static-analysis`,
`writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_state.c`, `runtime/native/rt_net.c`,
`runtime/native/rt_async_task.c`, `runtime/native/rt_async_channel.c`

---

## Files

- Modify: only files named by `03-refactor-audit.md`.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Reconfirm the chosen tranche still matches the current dependency graph.
2. Run behavior proof before edits.
3. Move or delete only one responsibility.
4. Run the same behavior proof after edits.
5. Record line-count deltas and dead-code proof if anything is deleted.

## Done

- At least one over-limit touched file is smaller or has a documented next
  split based on actual dependencies.
- No behavior change is mixed into the refactor.
