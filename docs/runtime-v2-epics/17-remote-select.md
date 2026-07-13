# Epic 17: Remote Select — DRAFT

**Status:** draft skeleton (2026-07-13); the central fork goes to second
opinion before the boundary decisions freeze.

## Why This Epic Exists

`select` over channels is the last first-class concurrency primitive
missing from the crossing surface. With `share()` shipped, fan-in
topologies are expressible (N producers, one selector) — the exact shape
select exists for. Local select is complete and row-covered
(`rt_async_select.c`: `rt_select_poll` over kinds/handles arrays,
per-task `wait_keys` + `select_timers`, loser-cleanup rows incl.
timeout-cleans-losing-waiter and cancelled-select-cleans-keys); remote
arms are the delta.

## Starting State (evidence)

- Local select: `InstrSelect` with arms {Task, ChanRecv, ChanSend,
  Timeout, Default} (`internal/mir/instr.go:319-350`), lowered via
  `lowerSelectExpr`; runtime `rt_select_poll(count, kinds, handles,
  values, ms, default_index)` (`rt_async_select.c:238`).
- Waiter stores are owner-shard-routed since the transport spine
  (`rt_waiter_route.c` maps waker keys to owner-shard stores) — but
  REGISTRATION is local-caller-side today; nothing registers a waiter on
  a remote shard's store.
- The anchored execute/reply discipline owns: single-pending retry,
  cancel-inflight with exactly-one reply-edge, orphaned-reply autonomous
  consumption, teardown termination — all row-proven across Epics 13-16.
- Sibling leases (Epic 16) make multi-channel fan-in legal at source
  level.

## The Central Fork (out for second opinion)

How does a remote arm wait?

- **Model A — cross-shard waiter registration.** Extend the waiter
  protocol so the selector's task registers channel waiters directly on
  owner shards (new transport op pair: register/cancel select waiter;
  wake already routes cross-shard). Select stays ONE local decision
  point; the channel-side machinery is reused as-is. Costs: waiter
  registration/cancellation becomes distributed state with its own
  races (register-vs-close, cancel-vs-wake in flight, double-wake
  suppression across shards); the local select fast path grows remote
  branches; failure modes concentrate in the waiter stores — the
  runtime's most delicate machinery (DEBT-027 neighborhood).
- **Model B — arms as anchored micro-ops, first-wins composition.**
  Each remote arm ships as an anchored operation (an "armed try":
  register owner-side, reply on fire); the select pending aggregates N
  sub-pendings; the first reply wins and the losers are cancelled
  through the PROVEN cancel-inflight/orphaned-reply discipline. Costs:
  N round trips to arm + up to N-1 cancels per select (heavy for hot
  loops); the "armed try" op is a new owner-side waiter-holding pending
  kind (a pending that parks a phantom waiter, not a body); win/cancel
  races multiply pendings rather than waiter-store states.
- **Hybrid C — owner-side proxy selector.** Ship ONE anchored block to
  a chosen owner shard that runs a LOCAL select over the co-located
  channels and replies with the winner; arms on other shards
  disqualify (first vertical: all far arms must share one owner shard,
  diagnosed kindly at sema). Trivially reuses local select wholesale;
  the restriction is honest and lifts later via A or B.

Constraints: dispatcher never blocks; the self-deadlock detector must
stay sound (a parked selector is a new suspect shape); kindness-first
diagnostics for every restriction; local-arm/remote-arm mixing rules
must be explicit from day one.

## Fixed Points

- Local select semantics and its cleanup rows are the contract to
  match; no fork of the local protocol.
- Timeout arms stay caller-side (the selector's own sleep store) in
  every model.
- Exactly-one winner, exactly-once loser cleanup, cancel-safe — the
  race matrix will be keyed on the same exactly-one discipline as the
  execute/reply epics.

## Candidate Slices (to be re-cut after the fork resolves)

1. Kickoff: fork resolution record, evidence re-pin, arm-surface sema
   design (mixing rules, diagnostics).
2. Runtime vertical for the chosen model with the race matrix
   (win-vs-cancel, close-vs-armed-arm, teardown-with-armed-selector,
   detector rows).
3. Sema + lowering + capability + e2e (fan-in from N shared producers,
   timeout beats empty channels, default arm short-circuits).
4. Stress/bench/closeout.
