# Epic 7 Task 11: The Peel — Shard Lanes Take Ownership

**Kind:** runtime code. **Depends on:** Tasks 8, 9, 10.

**Goal:** the control lock stops serializing the hot path. Two phases, each
in green committable steps:

**Phase A (guardian doubling).** Every access to shard-owned field groups
gains its shard/store lock while all paths still hold the control lock.
Correct by construction (control still serializes; the lane asserts catch
any two-shard mistake). Groups: A1 scheduler queues + running/wake counts;
A2 waiter stores (inside add/remove/pop/wake/migrate); A3 task park/status/
resume transitions in park_current and wake_task (wake reads `park_key`
under the owner lock and removes the stale store entry after releasing it —
value-based, absorbed by `wake_token`); A4 sleep stores; A5 `net_polling` +
poll scratch; A6 channel buffers under the channel owner's lock.

**Phase B (control drops per path).** Once a field group is consistently
shard-locked across all accessors, any single path may stop taking control:
the shard lock alone already excludes everyone. Steps: B1 the worker turn
(pop, net poll, sleep fire, yield requeue, park/wake commit; `mark_done`
splits into its shard phase plus a control epilogue taken only when scope/
await/free work exists, after the owner lock is released); B2 channel ops
(owner lock; foreign peers via control collect-then-wake with the
candidate/validate pattern so cancelled peers never consume values); B3 net
wait registration and poll build/complete on the owner lock; B4 the io
thread and N=1 runner move to shard 0's `poller_cv`, `io_cv` retires; B5
`rt_lock`/`rt_unlock` die — every remaining site names control explicitly.

The original blocking/await/shutdown scope folds in here: `done_cv` gating
(atomic await-waiter count), the blocking completion control->owner wake,
and the shutdown sweep are epilogue/lane details of B1/B4.

Deref discipline (D3) at every step: task pointers deref under control, or
under a shard lock that a shard-owned structure implies is the owner (own
queue entry, running-on-shard, same-shard `owner_hint`, own sleep store).

Static gates flipped by this task: `WorkerLoopShardLane`,
`ChannelOwnerShape`, `NoAmbiguousGlobalLock`, `GlobalCondvarRetirement`.

Every step runs the full proof set: c-check/cppcheck, the 9-mode behavior
suite at both shard configs, `runtime-v2-check` twice, `make check`,
LOC, and the lane asserts live in every test binary.
