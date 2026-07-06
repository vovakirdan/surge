# Epic 9 Task 3: RV2-DEBT-020 — Accept-Transition Join-Waiter Migration Proof

**Status:** planned; proof reset. Do not close or narrow `RV2-DEBT-020` until
this task completes with fresh evidence.
**Kind:** proof / analysis first; runtime code only if the proof exposes a real
stranding path.

## Why This Task Exists

`rt_waiter_migrate_join_waiters` moves `join_key(task)` waiters from a source
owner shard to a destination owner shard during `rt_task_replace_owner`. The
move extracts under the source shard lock and appends under the destination
shard lock, never holding both shard locks at once. Since Epic 8, a joiner's
`rt_task_poll` registration is target-owner-shard-local, not serialized by the
old global control lock. A joiner that registers during an owner-replace gap
could remain on the old shard while future completion wakes route to the new
owner.

Task 3 must prove whether this is reachable for accept-transition owner
replacement, or implement the smallest runtime fix that preserves the Epic 8
lane model.

## Known Risk In The Earlier Draft

An earlier local draft attempted to narrow this debt to a net-handle / stdlib
ABI question. That is not accepted. The proof was too broad and did not account
for all `rt_task_replace_owner` callers, especially the F2 adoption shape where
the re-owned task is the current joiner rather than the completed child.

This task therefore starts from the current code, not from the abandoned
narrowing conclusion.

## Required Evidence

- Enumerate every `rt_task_replace_owner` caller and classify the task being
  re-owned, its status at the call, and whether any `join_key` waiter can exist.
- Correctly distinguish:
  - F2 adoption (`rt_task_poll_adopt_placement`);
  - accept wake paths;
  - accept self-placement of `rt_current_task()`.
- Prove whether a `Task<TcpConn>` / accept task handle can be cloned or joined
  while the acceptor is still mid-accept.
- If handle escape is possible, add a deterministic proof window before changing
  runtime behavior. A new `SP_MIGRATE_GAP` sync point may be added only after
  this task justifies it.
- Update `rt_waiter_route.c` comments only after the proof is complete; no
  comment may carry an unproven "benign" assumption.

## Exit Criteria

- `RV2-DEBT-020` is either closed with a concrete proof, narrowed with an exact
  remaining blocker and owner, or fixed with deterministic positive and negative
  coverage.
- The task evidence names which join waiter can strand, or why no such waiter
  can exist.
- `make runtime-v2-syncpoint-check`, `make c-check`, and the relevant runtime
  behavior/static tests pass after any code change.
