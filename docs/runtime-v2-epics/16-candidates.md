# Next Epic Candidates (after the structural cleanup)

**Status:** decision note, 2026-07-12. One page per the three candidates
Epic 14's closeout named; direction is a product call.

## A. `@far_copy` — copyable far handles (RV2-DEBT-025)

Lift the affine-only restriction behind an opt-in capability so a far
handle can have multiple concurrent holders (send-capable copy, owner
lifetime unchanged; release semantics become refcount-or-epoch instead
of single-holder).

- Unblocks THREE recorded debts at once: the concurrent source-level
  park-retry proof, the multi-producer FIFO negative at source level,
  and real producer/consumer topologies (the first thing users will try
  with channels).
- Surface: sema capability + borrow/affinity rules, registry refcount
  semantics, teardown/idempotent-release rows; runtime machinery mostly
  EXISTS (the registry already survives release-vs-pin races).
- Risk: ownership-model design (copy vs borrow vs lease) needs the same
  care as Epic 11's affinity decisions; a wrong default here is
  user-visible forever.

## B. Remote `select`

`select` over far channels (mixed local/far arms, timeouts).

- High expressive value, but it COMPOSES far channels — with affine
  handles a select over two far channels needs two handles held by one
  task (legal), yet every interesting pattern (fan-in from multiple
  producers) still hits DEBT-025 first.
- Runtime: select arms over anchored ops need a multi-anchor pending
  protocol (new transport shape — the first vertical deliberately kept
  one pending per block).
- Verdict: naturally sequenced AFTER A.

## C. Migration (`move T to placement`)

Explicit shard migration of owned values.

- Independent of channels; unlocks the `@shard_movable` promise beyond
  crossings; big payload-representation overlap with the union-reply
  work (RV2-DEBT-030 family).
- Heaviest of the three: ownership transfer across heap cells touches
  accounting, drop machinery, and the payload wire format.

## Recommendation

A (`@far_copy`): three debts retired by one design, existing runtime
races already proven around the registry, and both B and C get easier
after it (B compositionally, C by narrowing payload work to
non-handle types first).
