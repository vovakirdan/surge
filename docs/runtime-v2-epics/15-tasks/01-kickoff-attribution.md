# Epic 15 Task 1: Kickoff — Attribution, Noise Bands, Move Inventory

**Status:** ready (2026-07-12).
**Kind:** evidence only — no production changes.

## Deliverables

1. **Metric semantics note.** What Sentrux `equality`, `redundancy`, and
   `quality_signal` actually measure (from tool output/docs/probing),
   recorded in the task doc so later thresholds argue from semantics,
   not folklore.
2. **Noise bands.** For each enforced scope and metric: the
   commit-to-commit variance over the last ~30 commits of the committed
   tree (scripted sweep over `git rev-list`, `sentrux check` per
   commit). Output: band width per metric, so task 4 can place
   thresholds N bands below the 3r operating point.
3. **Attribution.** Per failing metric, the per-file/per-edge
   contributors, obtained by ablation (measure, remove/merge a
   candidate cluster in a scratch worktree, re-measure) or by the
   tool's own reporting if it exposes one. Candidate clusters to probe
   first: the `rt_remote_task_*`/`rt_immediate_on_*`/`rt_far_channel*`
   family's shared-helper shapes (census walkers, pending-list scans,
   status mappers), the two debug-snapshot copies, and the harness
   testdata family (if in scope for the tool).
4. **Decision table.** One row per contributor: metric, scope, evidence,
   classification (real smell / inherent / resolver noise), disposition
   (fix / rebaseline / advisory), target task, owner, fallback. No
   enforced scope may be left failing without an owned row.
5. **Structural move inventory.** For every "fix" row: the concrete
   move (what merges, what splits, what boundary shifts), its expected
   metric effect, and its blast radius (files, exported symbols,
   headers). This is the artifact 1b reviews for liveness-path overlap
   and task 3 executes.

## Gates

Evidence-only: `make check` green (nothing should change); the task doc
carries the tables; Sentrux numbers quoted from the committed tree at a
named commit.

## Stop Conditions

- If ablation shows a metric responds to test-data or generated files
  rather than production structure, that contributor is classified
  resolver/tool noise and routed to task 4 — do not shape production
  code around it.
- If noise bands are wider than the current threshold gaps, say so
  plainly: the gates were flapping by construction, and task 4's
  re-baseline becomes the primary fix with task 3 reduced to the rows
  that remain real.
