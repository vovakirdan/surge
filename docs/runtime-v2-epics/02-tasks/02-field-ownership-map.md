# Epic 2 Task 02: Field Ownership Map

**Goal:** classify current `rt_executor` state before moving any field.

**Approach:** map every relevant field to runtime-level, shard-local,
compatibility, trace/debug, or later-epic ownership. The map must identify which
fields can move in Epic 2 and which fields wait for waiter, fd-registry,
allocator, or multi-shard epics.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_internal.h`,
`runtime/native/rt_async_state.c`, `runtime/native/rt_net.c`,
`runtime/native/rt_async_channel.c`

---

## Files

- Modify: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Create or modify: `docs/runtime-v2-epics/02-field-ownership-map.md`

## Steps

1. Inspect `rt_executor` and related runtime structs in
   `runtime/native/rt_async_internal.h`.
2. Search direct field users with `rg '->field_name|\\.field_name' runtime/native`.
3. Group fields by owner:
   - runtime lifecycle and process-level state;
   - `N=1` shard-local hot state;
   - blocking-pool compatibility state;
   - trace/debug counters;
   - later-epic state.
4. For each movable field group, write the preserved behavior boundary.
5. For each deferred field group, name the later owner epic and why it cannot
   move safely in Epic 2.
6. Record over-500-line file risks before code work begins.
7. Run `git diff --check`.

## Done

- No field group remains unclassified.
- The first code task has an exact field group.
- The map names test surfaces for each movable group.
- `NOTES.md` links the map and next task.
