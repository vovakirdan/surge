# Epic 16: Shared Far Handles (`share()` sibling leases) — DRAFT

**Status:** draft with the design fork RESOLVED (2026-07-12); the epic
DIRECTION (this vs remote select vs migration, `16-candidates.md`)
still awaits review before Task 1. Retires RV2-DEBT-025 and unblocks
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

## Open Questions For Kickoff

1. Release semantics per model (and the teardown matrix deltas).
2. Wire format: does a shared token need a holder epoch to keep stale
   double-release detectable per holder?
3. Multi-producer FIFO wording: what exactly is promised across
   producers after sharing (Epic 14 pinned lane-order-not-start-order).
4. The park-retry e2e and multi-producer FIFO negative move from
   "blocked" to acceptance rows here.
