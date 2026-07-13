# Epic 18 Task 3: Remote-Owner Rows — the Task-Side Obligation

## Design (locked before rows)

After the publish handoff the BODY TASK owns the shipped state. The
task-side twin of the pending reduction:

- `rt_task` gains `state_drop_fn_id` (0 = nothing to drop), filled by
  `rt_remote_spawn_create_body_task` from the pending at creation.
- **The first-poll rule**: the moment the body's poll actually STARTS
  (its first `__surge_poll_call` entry), the compiled body's own drop
  glue owns the state — the runtime clears the task's drop id right
  before the first poll dispatch. From then on, completion in any
  direction (success, cancelled mid-body) destructs captures through
  the body's glue exactly as local code would.
- While the id is still set (published but never polled), the task's
  terminal paths own the drop: `mark_done` for a
  cancelled-before-first-poll body (the poll header completes it
  without entering the body), task teardown for
  shutdown-with-unpolled-bodies, and
  `rt_remote_spawn_free_unpublished_task` for the created-but-never-
  published boundary (rows 13/14) — each drops exactly once because
  the id is nulled at the drop.
- Cross-cutting rows: nested exactly-once is the drop function's own
  recursion (proven at the compiler layer in Task 4's e2e); the census
  row asserts alloc/free balance across cells for every row above.

## Row-to-mode map (appended to `remote_task_behavior_drop.c` family)

- body cancelled before its first poll -> the task-side drop fires
  once (drop-cancel-before-first-poll);
- body RUNS (first poll clears the id) -> harness body destructs by
  hand; no runtime drop (drop-body-runs-owns-state — extends the
  handoff negative control);
- dispatch body-task alloc/publish failure -> dispatch-side drop
  restores pending ownership (structural: the failure paths re-set
  state_owned before the answer; a direct alloc-injection row is not
  buildable in this harness and is recorded as such);
- owner teardown with a published, never-polled body -> teardown drop
  fires once (drop-owner-teardown-unpolled);
- caller-teardown bound branch (deferred from Task 2): cancel routed
  to a published body that never polls -> exactly one drop between
  the cancel completion and teardown.

## Status

COMPLETE, with a scope correction discovered mid-task: Surge has no
user destructors — the drop function is a compiler-emitted recursive
FREE of heap-owning fields, so the glue-edge rows (body cancelled
mid-frame, nested captures, partial cleanup) are only observable as
allocator balance and belong to Task 4's compiled e2e where drop
functions actually exist. The runtime half proven here:

- drop-bound-cancel row: a droppable state handed to a PUBLISHED body
  never drops through the pending, even when the caller's cancel is
  routed and the body (parked via the binding recv) completes
  Cancelled — zero pending drops, clean census through releases.
- The publish-failure boundary (rows 13/14) is structural: the
  handoff clears ownership only AFTER `rt_remote_spawn_publish_body_
  task` returns OK, so every failure before that leaves the pending
  owner and the central drop site fires.
- Shutdown-with-unpolled-bodies follows the process-exit model (the
  executor is a process-lifetime singleton; task structs themselves
  are not reclaimed at shutdown) — recorded as the boundary, not a
  leak: census rows complete their flows.
- The task struct deliberately does NOT carry a drop id: no runtime
  path reads one (the first-poll rule collapsed into "the body's
  compiled glue owns from the first poll", which needs no runtime
  bookkeeping). Harness note: cross-thread pending observation needs
  the atomic mirror; plain-field polling is the trap the select rows
  already learned.
