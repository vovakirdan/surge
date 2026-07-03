# Epic 7 Task 2: Executor Lock Dependency Map

**Kind:** design map. **Depends on:** Task 1.

**Goal:** before any lock moves, record what `rt_executor.lock` protects
today — every field of `rt_executor`, `rt_shard`, `rt_task`, and `rt_scope`,
every lock acquisition site, every condvar, every waiter-key kind — with the
target lane for each, so Tasks 6-11 migrate against a written map instead of
rediscovering ownership mid-edit.

## Deliverable

`docs/runtime-v2-epics/07-executor-lock-dependency-map.md` with:

- the lock-site inventory by file and path;
- field-by-field lane tables (control / shard / atomic / immutable / tls /
  blocking), with `file:line` anchors at the baseline commit;
- the waiter-key ownership table (which store owns each key kind after the
  split);
- the path-by-path target-lane table for every current `rt_lock` caller;
- the hazard list the split must not recreate (wake-vs-park, stale
  `park_key`, duplicate enqueue, accept-winner cleanup, channel value loss on
  cancelled peers, inline child poll, compensation workers, init ordering);
- the already-safe list (atomics, TLS, immutable config, blocking pool, heap
  cells).

Decisions the map may not make: anything marked *(spike)* belongs to Task 3
(clock protocol, task lifetime rule, scope-key store, re-placement protocol,
condvar fates, non-user polls under shard lock). The map records the options;
the spike picks.

## Out Of Scope

- No code changes, no test changes.
- No final locking-protocol decisions (Task 3).

## Checks

Docs-only: `git diff --check`. Anchors verified against the baseline commit
by reading the cited lines.

## Success Criteria

- Every field in the four structs appears in exactly one lane row.
- Every `rt_lock` caller file appears in the inventory and the path table.
- Task 3 can enumerate its open decisions directly from the *(spike)* marks.
- Evidence ledger and `NOTES.md` updated; index status flipped.
