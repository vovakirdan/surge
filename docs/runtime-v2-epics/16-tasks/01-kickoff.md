# Epic 16 Task 1: Kickoff — Evidence And The Lease-Table Design Note

**Status:** complete (2026-07-13).
**Kind:** evidence only; no production changes.

## Evidence (pinned at commit `3a8b65a8`)

- **Registry entry today** (`rt_far_channel.c:21-29`):
  `{channel, id, generation, owner_shard_id, state (OPEN/RELEASING),
  inflight, next}` — ONE generation per entry; the token IS the holder.
- **Token ABI** (`rt_remote_spawn.h:37-42`): `{task_id, generation,
  owner_shard_id, kind}` — 4x u64-class fields shared by task and
  channel kinds; channel tokens put the registry id in `task_id`.
- **Validation layers** (Epic 15): `generation_checked_locked`
  (`rt_far_channel.c:498`) and `live_open_locked` (`:507`), used at
  resolve (`:144`), release (`:195`), pin (`:253`). These become
  LEASE-aware lookups: the id addresses the entry, the generation
  addresses a lease row.
- **Transport kind space** (`rt_transport.h`): 13 is the last used kind;
  the share pair takes `FAR_CHANNEL_SHARE_REQUEST = 14` (data lane) /
  `FAR_CHANNEL_SHARE_REPLY = 15` (control lane), with counters mirroring
  the create pair.
- **Dispatch template**: the channel-create execute/reply discipline
  (`rt_far_channel.c` create + dispatch_create; caller-allocated handle
  out-param, reply-once, retry via the shared pending) is the exact
  shape `share` reuses — destination = the anchor's owner shard, like
  every anchored op.
- **Detector assumptions that break with multiple holders**
  (`rt_remote_task_deadlock.c:124-217`): the suspect scan qualifies any
  channel-parked bound body under global quiescence. With one holder
  that implied "no in-model waker". With siblings the implication
  SURVIVES AT QUIESCENCE (a runnable sibling makes some shard
  non-quiescent, so quiescence still proves no waker) — but the
  adversarial rows must PROVE the false-negative guard (holder parked
  while a sibling is runnable => no panic) and the message must name
  the lease topology instead of implying a sole holder.

## Lease-Table Design Note (the task-2 contract)

Entry becomes:

    entry { channel, id, owner_shard_id, pins (inflight),
            leases: list of { lease_id, generation, state ACTIVE|RELEASED } }

- `generation` moves from the entry to the lease; the mint allocator
  (shared next_request_id) issues both lease ids and generations, so no
  two leases ever share a (lease_id, generation) pair and stale
  detection stays exact per sibling.
- The FIRST lease is minted by `channel_on` itself (the creating holder
  is lease zero) — one code path for all lease validation from day one.
- Reclaim invariant: entry reclaimable iff every lease is RELEASED and
  pins == 0. Releasing a lease never reclaims directly; the existing
  unlock-then-reclaim tail (Epic 15) gains the lease-count condition.
- `share` is an anchored owner op: validate + pin the source lease,
  mint a sibling lease under the registry lock, reply with the sibling
  token; cancel-mid-share consumes the orphan sibling autonomously
  (the orphaned-reply discipline, unchanged).
- Token compatibility: existing single-holder programs see identical
  behavior — one lease, release drains it, the entry reclaims; every
  Epic 14/15 row must stay green UNCHANGED before any new row lands
  (the behavior-neutrality bar for the data-model commit).

## Sentrux Baselines (committed tree, `3a8b65a8`)

root 6182, internal 6506, runtime 5315, runtime/native 5405 — all four
scopes pass under the noise-band thresholds.
