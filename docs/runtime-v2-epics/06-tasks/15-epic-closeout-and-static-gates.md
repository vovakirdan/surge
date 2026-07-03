# Task 15: Epic Closeout And Static Gates

**Status:** Draft
**Kind:** closeout
**Depends on:** all prior Epic 6 tasks

## Context

This mirrors Epic 4 Task 15 (`04-tasks/15-epic-closeout-and-static-gates.md`)
and Epic 5's closeout: consolidate durable notes into permanent documents,
state exactly which `RUNTIME_V2.md` contracts are now implemented versus
still deferred, and hand off cleanly to whatever comes next. Epic 6's
closeout is more consequential than prior epics' because it is the point
where the epic's own "Next Runtime Handoff And Syntax Gate" section becomes
active: Epic 7 (lock splitting) and any future language-syntax epic both
depend on this closeout being precise about what is proven versus assumed.

## Goal

Close Epic 6 with durable evidence, an accurate accounting of every Accept
Ownership Contract and Performance Contract bullet, and a precise Epic 7
handoff plus a restated syntax gate.

## Why This Task Exists

`RULES.md` Global Rule 10 requires durable decisions to move out of `NOTES.md`
into owning documents before an epic closes — this task is that
consolidation point. It is also the only place in the epic where "is Epic 6
actually done" gets asked against the full Epic Acceptance checklist in one
pass, rather than task-by-task.

## Scope

- Run final standing gates: `make c-check`, `make cppcheck`,
  `make runtime-v2-check` (including the new `runtime-v2-accept-check` from
  Task 13), `make check`, `git diff --check`, Sentrux root/`runtime/`/
  `runtime/native/` scans.
- Walk every bullet in the epic document's Accept Ownership Contract,
  Performance Contract, and Epic Acceptance sections; mark each Done, Done
  with recorded debt, or Blocked with the exact blocking evidence — no
  bullet may be left unaddressed or assumed.
- Copy durable decisions out of `NOTES.md` into: this epic document (e.g.
  final listener-model decision, final poller-ownership model, final
  `RT_RUNTIME_MAX_SHARDS` value), `README.md` (roadmap/status), `DEBT.md`
  (any new or closed debt), and `docs/RUNTIME_V2.md` if any target-
  architecture section needs a "completed by Epic 6" note analogous to the
  existing "Completed by Epic 5" note in §11 (Allocation And Heap Stats) and
  Phase 2.5.
- Record final line counts for every file this epic touched, and the final
  `.loc-legacy-allowlist` state after Task 14's refactor tranche.
- Record final Sentrux `quality_signal` for root/`runtime/`/`runtime/native/`
  against the Task 1 baseline, and state whether quality improved, held, or
  needs a recorded exception.
- State explicitly which `RUNTIME_V2.md` contracts are now implemented
  (accept ownership, per-shard fd registry population for `N>1`, per-shard
  poller/wake, Tier 1 no-steal for connection tasks) and which remain
  deferred, split by owner: splitting `rt_executor.lock` and moving the
  remaining global-compatibility primitives (channels, join, scope wake,
  cancellation, blocking completions, timers) to shard-owned state is
  deferred to Epic 7, not to "Phase 4" — the roadmap in `README.md` assigns
  lock splitting to Epic 7 specifically. Cross-shard messaging,
  `far`/`submit_to`, remote-free, and `io_uring` remain Phase 4+ work owned
  by later epics (8, 9, 10 per the roadmap). Do not let the closeout imply
  more is done than is actually done, and do not blur the Epic 7 vs. Phase 4+
  distinction — they are different future epics with different scopes.
- Write the Epic 7 handoff: the preserved global-lock boundary, the exact
  shard/worker/task-ownership shapes Epic 7 will need to split further, and
  any open question this epic's tasks flagged but did not resolve (e.g. any
  Task 8 `RV2-DEBT-010` decision, any Task 9 non-owner-access guard-vs-debt
  decision, any Task 10 Phase 4 temptation notes).
- Restate the syntax gate from the epic document's final section: any later
  syntax or crossing epic needs a dedicated language-surface discussion with
  the user first; `far`, `submit_to`, `crosses`, `shard-movable` remain
  semantic placeholders.
- Update `docs/runtime-v2-epics/06-tasks/README.md`'s status column for all
  15 tasks to `Complete` (or `Blocked` with evidence, if any task could not
  fully close).

## Out Of Scope

- Any new implementation work — if a gap is found during closeout review, it
  becomes either a fast-follow fix inside the relevant still-open task (if
  one is still open) or an explicit `DEBT.md` entry, not an ad hoc patch
  inside this closeout task.

## Approach / Steps

1. Run every final standing gate listed in Scope.
2. Walk the Accept Ownership Contract, Performance Contract, and Epic
   Acceptance checklists bullet by bullet; mark each explicitly.
3. Consolidate `NOTES.md`'s durable content into the epic document,
   `README.md`, `DEBT.md`, and `docs/RUNTIME_V2.md` as appropriate.
4. Record final line counts and Sentrux signals.
5. Write the explicit implemented-vs-deferred contract accounting.
6. Write the Epic 7 handoff section.
7. Update `06-tasks/README.md` task statuses.
8. Update `docs/runtime-v2-epics/06-evidence.md` with the closeout entry and
   a `## Result` summary (matching the style of Epic 4's Task 15 `## Result`
   section in `04-tasks/15-epic-closeout-and-static-gates.md`).

## Files

Touch:

- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
- `docs/runtime-v2-epics/06-evidence.md`
- `docs/runtime-v2-epics/06-tasks/README.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/DEBT.md`, if wording is stale
- `docs/RUNTIME_V2.md`, if a target-architecture section needs a
  completed-by-Epic-6 note

## Skills & Working Practice

- This task benefits from a fresh, skeptical read rather than momentum from
  the implementation tasks — if a subagent runs it, it should independently
  re-check a sample of the Accept Ownership Contract bullets against actual
  code/tests rather than trusting Tasks 6-14's self-reported Definition Of
  Done checkboxes at face value.
- Do not soften language on any deferred or blocked item to make the epic
  look more complete than it is; the whole point of this closeout, per
  Global Rule 10, is that future readers trust it without re-verifying
  everything themselves.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- All stable multi-shard accept tests (Task 13's gate)
- Native net benchmark (final confirmation run)
- `git diff --check`
- Sentrux root, `runtime/`, and `runtime/native/` scans

## Definition Of Done

- [ ] Every Accept Ownership Contract, Performance Contract, and Epic
      Acceptance bullet is marked Done, Done-with-debt, or Blocked with
      evidence.
- [ ] `NOTES.md`'s durable content is consolidated into the owning documents;
      `NOTES.md` itself is left as a live working log, not the sole record of
      any final decision.
- [ ] Final line counts and Sentrux signals are recorded against the Task 1
      baseline.
- [ ] The implemented-vs-deferred `RUNTIME_V2.md` contract accounting is
      explicit and does not overstate completion.
- [ ] The Epic 7 handoff names the preserved global-lock boundary and every
      open question flagged by Tasks 6-14.
- [ ] The syntax gate is restated plainly.
- [ ] `06-tasks/README.md` and `README.md` reflect the final epic status.

## Result

(Fill in after closeout runs — do not pre-write this section; it must
reflect what actually happened, following the pattern of Epic 4 Task 15's
`## Result` section.)

## Evidence To Record

- `06-evidence.md`: full closeout entry using `EVIDENCE_TEMPLATE.md`'s shape,
  plus a final `## Result` paragraph.
- `README.md`: updated Epic Roadmap row and Current Status paragraph for
  Epic 6, matching the style used for Epics 1-5.
- `DEBT.md`: final state of every debt item this epic touched or created.
