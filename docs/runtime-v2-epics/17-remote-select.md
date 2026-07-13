# Epic 17: Remote Select — DRAFT

**Status:** review-ready (fork resolved 2026-07-13, external review).
Sequencing note: the stress-epoch triage REPRODUCED the RV2-DEBT-027
double-poll race (~4% per run on a retained binary — see the debt row);
the park/wake machinery this epic parks selectors on is the flaking
machinery, so the fix-first-vs-parallel decision gates Task 1.

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

## Fork Resolution (second opinion, 2026-07-13): Model C first

External review (Codex, grounded in the runtime contract doc, plus the
reviewing agent's synthesis) resolved the fork:

- **Vertical 1 ships C** (owner-side proxy selector): lowest race
  novelty (one anchored request/reply, zero new race shapes), one
  remote transaction per select regardless of arm count (the hot
  single-owner receive loop — the primary usage — is served
  permanently), and wholesale reuse of local select.
- **Linearization moves to the owner lane — documented, not unsound.**
  The runtime contract frames remote select's predictability purely as
  cost; no caller-lane FIFO/fairness promise exists anywhere. Vertical
  1 WRITES the explicit clause: no caller-lane ordering or fairness
  across the shard boundary; the winner is decided on the owner lane
  exactly where the owner's own local select would decide it.
- **Sema restriction, kindness-first**: all far arms of one select must
  share one owner shard in vertical 1 (mixing with local arms and
  multi-owner selects deferred); the diagnostic names the restriction
  and the split-into-selects workaround — the anchored-body shape-rule
  precedent, applied again.
- **Lift path**: C stays the permanent single-owner fast path ->
  a stabilization vertical extracts the reusable SELECTOR LIFECYCLE
  (winner arbitration, terminal transition, cancellation, stale-wake
  suppression, orphaned-reply consumption, detector representation)
  before anything distributed is built -> B (arms as anchored
  micro-ops) becomes the honest slow path for the multi-owner tail ->
  A (cross-shard waiter registration) is built ONLY if profiling shows
  the multi-owner tail is hot, and never before a dedicated
  waiter-store hardening vertical (remote select must never be the
  first consumer of a waiter-store change — that neighborhood carries
  the RV2-DEBT-027 flake).
- **Detector requirement (all models, smallest in C)**: the new wait
  chain `caller selector parked -> remote select op -> owner-side
  channel waiter` must collapse into ONE logical selector op in the
  suspect scan, or the detector both false-positives (both ends
  parked) and false-negatives (the proxy looks like independent
  progress).

## Acceptance Race Matrix (C, contract-level; every row test-owned)

Each row asserts exactly one terminal outcome, one visible reply edge,
no anchored-block leak, no owner-waiter leak, no second selector
resume: (1) ready-before-execute; (2) park-vs-send one-wake-one-winner;
(3) ready-vs-ready tie-break matches local rt_select_poll, losers
cleaned; (4) registration-vs-close; (5) park-vs-close wakes exactly
once; (6) cancel-before-owner-registration; (7) cancel-after-
registration; (8) cancel-vs-wake-in-flight (exactly one of winner/
cancel visible, the other orphaned and consumed); (9) duplicate
execute/retry mints no second proxy or waiter; (10) stale-generation
wake consumed and ignored; (11) lease invalidation while parked ->
diagnosable terminal error + owner cleanup; (12) sibling-lease
concurrency wakes only the right selector; (13) caller teardown vs
reply (autonomous consumption); (14) owner teardown with an anchored
selector pending (dispatcher never blocks); (15) detector
false-positive guard (external producer runnable -> silent);
(16) detector true-positive (quiescence -> report names the selector
shape). Rows 8/10/13 exercise the proven exactly-one-reply-edge
discipline — the reason C is cheap.

## The Original Fork (retained for the record)

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

## Planned Slices (re-cut per the resolution)

1. Kickoff: evidence re-pin; the contract clause (no caller-lane
   ordering) lands in the runtime contract doc; sema surface design
   for the single-owner restriction with its diagnostics.
2. Runtime vertical: the proxy-selector anchored op (select ships as
   one anchored block running local select owner-side; timeout arms
   stay caller-side) with matrix rows 1-14.
3. Detector: the chain-collapse representation with rows 15-16.
4. Sema + lowering + capability + e2e (fan-in from N shared producers
   on one owner, timeout beats empty channels, default short-circuits,
   single-owner restriction diagnosed kindly).
5. Stabilization: extract the selector lifecycle behind a seam (the
   artifact B and A consume later); bench; closeout. The B/A tail work
   is explicitly OUT of this epic (recorded as a debt row with the
   profile-gated condition).
