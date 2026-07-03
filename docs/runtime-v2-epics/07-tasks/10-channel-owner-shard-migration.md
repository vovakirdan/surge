# Epic 7 Task 10: Channel Owner-Shard Migration

**Kind:** runtime code. **Depends on:** Task 8.

**Goal:** channels get their owner shard (D5 channel slice): fixed at
creation to the creating task's shard (shard 0 outside tasks), with channel
waiter keys routed to the owner's store. Channel operations stay on the
control lane: task-state fields must switch guardians atomically across all
their accessors, so the "channel ops never take the control lane" gate
flips at the Task 11 peel together with the other lane gates (flip plan
updated in the Task 5 document).

## Scope

- `struct rt_channel` gains `owner_shard_id` (never changes; channels are
  never freed, so key resolution via the embedded pointer is stable);
  `rt_channel_new` derives it from `rt_current_task()`.
- `rt_channel_owner_shard_id` accessor; the waiter-store resolver routes
  `WAKER_CHAN_SEND/RECV` to the owner shard's store, replacing the shard-0
  compatibility arm.
- Owner-local waiter stub harness gained the accessor stub.

## Checks

c-check/cppcheck pass; channel-focused behavior modes (cross-channel FIFO
+close, close-wakes, select-across-owners, cross-cancel) green;
`runtime-v2-check` pass twice (includes the cancelled-waiter channel
contracts); `make check` pre-commit; `git diff --check`.

## Success Criteria

Channel keys live with their owner; behavior unchanged; evidence/NOTES/
index updated; own commit.
