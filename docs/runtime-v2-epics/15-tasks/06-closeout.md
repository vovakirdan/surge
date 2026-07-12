# Epic 15 Task 6: Closeout

**Status:** complete (2026-07-12).

Release invariants, all verified on the committed tree at `7435e714`:

- **Clean-clone reproduction**: `make check` green twice in a pristine
  worktree; the full `runtime-v2-check` umbrella ran twice there — run
  one surfaced exactly one REAL find (the parked-with-work source gate
  still anchored to the pre-lock-split worker shape; rotted while
  unwired, fixed and re-anchored to `rt_worker_turn.c`), run two failed
  only `TestRuntimeV2CancelledBlockingWaiterDoesNotConsumeCompletionWake`
  with the RV2-DEBT-018 signature (empty stderr, in-suite only, 5/5
  green focused — recurrence noted in the debt row). Every other gate in
  both runs green.
- **Sentrux stability**: two consecutive runs identical on all four
  scopes (root 6182, internal 6506, runtime 5315, native 5405); all
  scopes pass their re-placed rules.
- **Symbol census**: 686 exported `rt_*` symbols, byte-identical to the
  epic's start — the structural pass moved shapes, not surface.
- **Artifact-free tree**: `git status` clean of tracked modifications.

## Epic Outcome Summary

| metric | epic start | closeout |
| --- | --- | --- |
| native `min_redundancy` | 0.2491 FAILING (gap < 1 commit step) | 0.2514 passing, threshold 0.245 (3 noise bands) |
| runtime `min_redundancy` | 0.2510 knife-edge (flapped in-window) | 0.2513 passing, threshold 0.245 |
| native quality | 5394 | 5405 (operating point recorded; pre-family target retired) |
| root scope | 2 failing metrics gating on 46% resolver noise | advisory floors + written re-promotion condition |
| gate integrity | 2 known rot incidents, manual discipline | mechanized: gatecheck in `make check`; 14 orphans wired; 2 invisible breaks found and fixed (fd-registry link, park source gate) |
| RV2-DEBT-027 | deferred untouched | bounded: 50/50 + 3/3 TSan green, quarantined target, owner + handoff |
| naming debt | 8 fixture headers + stragglers | zero `Epic N` refs outside docs/; plan closed |

Debt: RV2-DEBT-028 closed (re-baselined), RV2-DEBT-029 closed
(advisory), RV2-DEBT-027 bounded and handed off with an exit criterion.
