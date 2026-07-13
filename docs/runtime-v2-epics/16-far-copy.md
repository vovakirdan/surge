# Epic 16: Shared Far Handles (`share()` sibling leases) — DRAFT

**Status:** COMPLETE (2026-07-13). All slices landed same-day; closeout
below. Force-close (slice 2) deferred behind its own design review
(RV2-DEBT-032); select/migration lease work (slice 3) rides those
epics.

## Closeout (2026-07-13)

- **Acceptance met.** `ch.share()` end to end on LLVM at SHARDS=1,2,8:
  the fan-out e2e runs two producers on sibling leases parking on a
  capacity-1 channel with cross-holder drain — the concurrent compiled
  park-retry proof RV2-DEBT-025 blocked — with cross-producer arrival
  asserted as a set (owner-lane order stays the only promise). The
  matrix rows are behavior-suite-owned and green repeatedly: sibling
  round trip with trace counters, per-lease release independence with
  exact double-release staleness, stale share through a released lease,
  the pin outliving every lease to the reply edge, teardown draining
  all leases, and the lease census backing each row.
- **Detector re-grounded without touching quiescence.** The soundness
  argument survives multiple holders (a runnable sibling keeps its
  shard non-quiescent); the panic names the lease topology ("has N
  leases but every holder is idle"); adversarial rows pin the true
  two-holder deadlock, the runnable-holder false-negative guard, and
  the deadlock after a peer released.
- **Kindness surface.** `share()` types as a borrowed mint (original
  stays usable — pinned by a reuse row); misuse diagnostics: SEM3175
  arity, FUT7019 sync-context, FUT7021 off-LLVM, and the
  use-after-move hint now says "call share() before moving it so each
  holder keeps its own lease".
- **Bench.** share-mint 112068/11925/11186 mints/s at 1/2/8 shards
  (8.9/83.9/89.4 us) — within the immediate-on band even with 2000
  leases accumulating on one entry (the linear lease walk does not
  dominate at this scale; a lease-table index is future work if
  topologies grow beyond thousands of holders per channel).
- **Debt.** RV2-DEBT-025 closed as superseded (copyable tokens rejected
  by review; the need met by sibling leases); RV2-DEBT-032 opened for
  force-close behind a design review.
- **Sentrux** (committed tree at closeout): root 6177, internal 6499,
  runtime 5303, native 5392 — all scopes pass the noise-band
  thresholds; deltas within the recorded bands.
 Retires RV2-DEBT-025 and unblocks
the concurrent source-level park-retry proof, the multi-producer FIFO
negative, and real producer/consumer topologies.

## Design Decision (second opinion, 2026-07-12): Model B

External review (Codex, high effort) selected the explicit `share()`
model and rejected both alternatives on grounds we adopt verbatim:

- **A (copyable token + refcount) is disqualified by identity, not
  taste**: byte-identical copies make first-release, sibling-release,
  and double-release indistinguishable at the registry; every fix
  either breaks other holders or reinvents per-holder identity (= B
  with worse semantics). It also moves misuse detection from sema to
  runtime refcount failures — violating the kindness-first contract.
  B keeps a per-sibling (lease id, generation), so a double release of
  one sibling is exactly stale-detectable, independent of the rest.
- **B is the smallest runtime change**: the reclaim invariant becomes
  "entry reclaimable iff active_lease_count == 0 && pin_count == 0";
  releasing a lease marks THAT lease released and never reclaims the
  entry directly; the proven release-vs-pin protocol is untouched.
- **C stays dead** per the far-borrow exclusion (SEM3138) and
  non-lexical task lifetimes.

Two hard rules adopted: no ordinary holder may force-close (release
means "I am done", never "destroy for everyone"; force-close is a
distinct owner capability, its own slice), and self-deadlock detection
must ground in the real task/lease wait graph, never handle identity.

Synthesis refinements recorded as design inputs:

1. **`share()` is an anchored owner operation**, not a local token
   copy: it mutates the owner-side registry, so it rides the same
   pin/linearization machinery as every anchored op — one owner round
   trip per minted sibling, paid at topology construction.
2. **The primitive is `&self` minting** — `ch.share()` leaves the
   original lease intact and returns a fresh affine sibling; the
   consuming two-way split is sugar. Canonical fan-out:
   `for _ in 0..n { spawn worker(ch.share()); }`.
3. **The concrete data-model slice**: the registry entry grows from
   {generation, pins} to {pins, active_lease_count, lease table:
   lease id -> (generation, state)}; GENERATION MOVES PER-LEASE — that
   is the true blast radius on today's generation-check code and the
   first vertical.
4. **Self-deadlock re-grounding is its own load-bearing concern**, not
   one matrix row: with multiple holders the current heuristics can
   false-negative (blocked on another holder's future op is NOT a
   deadlock) — the detector moves to a (task, lease) wait-graph
   argument with adversarial multi-holder rows, riding slice 1.

Failure-mode ledger (from review, to become matrix rows): leaked lease
= liveness bug not UAF; share-vs-final-release; double release;
cross-sibling confusion; close-vs-share; owner teardown with active
leases; cancellation leaks; generation exhaustion fails closed; select
registration leaks; migration identity loss (lease identity must stay
SEPARATE from location epochs — never reuse lease generation as a
migration epoch).

Slicing: (1) lease-table model + `&self` share() + per-lease release +
the "moved handle -> call share()" sema hint + core matrix rows +
detector re-grounding; (2) force-close capability distinct from
release; (3) select/migration lease preservation with separated
identity/location epochs.

## The Original Fork (retained for the record)

How does a far handle become shareable?

- **Model A — copyable token, refcounted registry entry.** `@far_copy`
  types get implicit copy of the token; the registry entry carries a
  holder count; `release` becomes drop-last-holder (or an explicit
  `close`-style owner right). Cheap copies, but release semantics
  change shape (who may release? every holder? only the minting
  holder?), and the entry lifetime becomes distributed state.
- **Model B — explicit `share()` producing a counted lease.** The
  handle stays affine; `share()` mints a sibling token with its own
  registry lease (id+generation per sibling). Explicit at every split
  point (reads like ownership), one more token mint per share, release
  per sibling with the entry reclaimed when all sibling leases and pins
  drain.
- **Model C — borrow-scoped access (`&far T` in task spawns).** No
  copies; a borrow-checked lending of the handle into child tasks whose
  lifetimes are bounded by the lender (structured concurrency). Closest
  to the language's ownership story, but far borrows were explicitly
  rejected in the far-type design (SEM3138) and task lifetimes are not
  lexically bounded today.

Constraints from shipped machinery: the registry already survives
release-vs-pin races (pins defer reclamation to the reply edge); tokens
are 4x u64 value structs already copied freely inside the runtime; the
affine surface rule lives in sema (move-on-use), so the change is
capability-gated at the type level either way.

## Fixed Points (not up for debate)

- Owner-side linearization and the anchored-op surface are unchanged.
- The self-deadlock detection contract must remain sound with multiple
  holders: quiescence + a parked body still implies no in-model waker.
- Kindness-first: mis-sharing diagnostics name the fix at sema
  (decision-8 template).

## Race And Failure Matrix (contract-level draft; every row test-owned before any flip)

1. share-vs-final-release: a sibling minted concurrently with the last
   other lease releasing — either the mint lands on a live entry or it
   answers stale; never a lease on a reclaimed entry.
2. share-vs-pin/reclaim: minting while an anchored block holds a pin;
   release-all during a mint round trip.
3. double release of one sibling -> exactly one stale-token answer;
   other siblings unaffected (per-lease generation proof).
4. cross-sibling confusion: ops through sibling X after sibling Y
   released; X unaffected.
5. leaked lease: a task exits without releasing — entry stays live
   (documented liveness debt, not UAF); census row proves no reclaim.
6. owner teardown with active leases on remote shards: deterministic
   stale answers to every sibling, no waiter left parked.
7. cancellation mid-share: caller cancelled during the mint round trip
   -> the orphaned lease is consumed autonomously (mirrors the
   orphaned-reply discipline).
8. concurrent producers park-retry (the RV2-DEBT-025 unlock): two
   source-level holders, capacity-full park, drain by the other holder
   — the compiled park path proof Epic 14 could not express.
9. multi-producer FIFO negative at source level (lane order, not
   share order — extends the Epic 14 harness row).
10. self-deadlock adversarial set: (a) true deadlock with two holders
    both parked on the same channel with no third waker -> panic names
    the lease topology; (b) NOT-deadlock: holder A parked while holder
    B is runnable elsewhere -> no panic (false-negative guard);
    (c) sibling released while its former peer is parked.
11. generation exhaustion on one lease id fails closed.
12. trace counters: share mints/replies counted; fallback tripwire
    stays zero; leak census extended to lease tables.

## Acceptance Criteria (draft)

- `ch.share()` works end to end on LLVM at SHARDS=1,2,8: N-way fan-out
  producer/consumer source program with park-retry through compiled
  bodies (the row Epic 14 recorded as blocked).
- The matrix above is test-owned and green twice; the leak census
  covers lease tables.
- Sema diagnostics: "use of moved far handle: call `share()` before
  moving it into multiple tasks" with span-precise hints; misuse never
  degrades to a runtime-only failure (kindness-first).
- Self-deadlock detection re-grounded on the (task, lease) wait graph
  with the adversarial rows green; the FFI opt-out contract unchanged.
- Bench row: share-mint cost at topology construction; steady-state
  anchored-op cost unchanged from the Epic 14 baseline within noise.
- Gates: transport umbrella + gatecheck green twice; goldens diag-only.

## Stop Conditions

- If the per-lease generation move cannot keep the existing
  stale-token rows green unchanged, stop and re-review the data model
  (the token wire format is a public ABI surface).
- If detector re-grounding cannot prove the false-negative guard
  (row 10b) without walking cross-shard state from the caller side,
  route to design review rather than weaken the quiescence contract.
- Force-close semantics questions route to slice 2's own review; slice
  1 ships with release-only semantics.

## Open Questions For Kickoff

1. Release semantics per model (and the teardown matrix deltas).
2. Wire format: does a shared token need a holder epoch to keep stale
   double-release detectable per holder?
3. Multi-producer FIFO wording: what exactly is promised across
   producers after sharing (Epic 14 pinned lane-order-not-start-order).
4. The park-retry e2e and multi-producer FIFO negative move from
   "blocked" to acceptance rows here.
