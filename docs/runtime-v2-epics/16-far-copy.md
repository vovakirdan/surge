# Epic 16: Copyable Far Handles (`@far_copy`) — DRAFT

**Status:** draft skeleton (2026-07-12); direction awaits review (see
`16-candidates.md` for the alternatives). Retires RV2-DEBT-025 and
unblocks the concurrent source-level park-retry proof, the
multi-producer FIFO negative, and real producer/consumer topologies.

## The Central Design Fork (out for second opinion)

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
