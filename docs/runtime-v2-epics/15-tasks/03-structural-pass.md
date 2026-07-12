# Epic 15 Tasks 3 + 3r: Structural Pass And Remeasure

**Status:** complete (2026-07-12).

## Sub-steps (each a real commit, measured on the committed tree)

| commit | move | native redundancy | native quality |
| --- | --- | --- | --- |
| (start) | — | 0.2491 (FAILING) | 5394 |
| `1b3a0a19` | FC-1: layered token validation (generation-checked + live-open lookups; resolve/pin/release predicates stated once) | 0.2508 | 5402 |
| `d194306b` | FC-2: one reclaim contract (RELEASING + zero pins, shared unlock-then-free tail across release/unpin/shutdown sweep) + one immediate-on answer helper (four failure replies collapsed) | 0.2514 | 5405 |

Behavior-neutrality proof per sub-step: exported `rt_*` symbol census
(686 symbols) byte-identical before/after; full behavior suite,
self-deadlock rows, transport gate, c-check/c-warnings/cppcheck green;
`rt_immediate_on.c` stays inside the 300-line family pin (296).

## Decision-table dispositions

- **N1 (native min_redundancy)**: FIXED — the failing gate clears at
  0.2514; residual margin (~1 noise band) RECLASSIFIED to task 4 for
  noise-band threshold placement, per the row's recorded fallback.
- **N3 (runtime min_redundancy)**: FIXED by the same moves (0.2513);
  placement residual to task 4.
- **N2 (native quality vs DEBT-028's 5484)**: RECLASSIFIED to task 4 —
  the moves recovered +11 points (5394 -> 5405); the remaining distance
  is the transport family's inherent import/call coupling (the gate
  attributes coupling 0 -> 0.10; the complex-function census is dominated
  by legacy bignum/fs/net outside this epic's scope). Honest dedup
  cannot reach a number recorded before the family existed.
- No real smells were attributed to `internal`; its margins are wide
  (task 1 tables).

## 3r: Remeasure

Post-cleanup operating points (committed tree at `d194306b`): native
redundancy 0.2514 / quality 5405; runtime redundancy 0.2513 / quality
5315; internal unchanged (6506); root unchanged (0.4484 / 0.7992 /
6182 — the moves are invisible at root scope, consistent with the
resolver-noise classification). Commit-to-commit noise bands from the
task-1 30-commit sweep stand (the two cleanup commits sit inside the
same band widths): redundancy step-max 0.0015 (native/runtime), 0.0013
(internal), 0.0010 (root); equality step-max 0.0012/0.0005/0.0003.
Task 4 consumes these numbers.
