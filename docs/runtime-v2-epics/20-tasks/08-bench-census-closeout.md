# Epic 20 Task 8: Bench + Census + Debt Closeout

The epic's closing task. User decision (2026-07-19): RV2-DEBT-052 is
FIXED FIRST so the census rows assert an honest strict zero, instead
of weakening the epic's bar to the far==local differential form.

## Rows (order = execution order)

1. **RV2-DEBT-052 fix.** `compare` over an owned boxed union frees
   its scrutinee box exactly once on every arm. Root cause is the
   DEBT-040 class: the normalize pass synthesizes the `__cmpN`
   scrutinee LET after sema computed drop obligations, so no release
   ever fires. Fix shape: per-arm synthesis in `normalizeCompareExpr`
   — an arm that MOVED the payload out releases the box shallowly
   (the DEBT-040 release channel); an arm that leaves the payload
   inside (no binding, `_`, no-payload tag, the non-exhaustive
   fallback) deep-drops the scrutinee through normal drop glue.
   Verification gate #1: sema's scrutinee consumption semantics —
   the release is sound only where the scrutinee's ownership really
   transferred into the compare.
2. **Census e2e, strict zero.** Union-compare census row directly
   over an await window; the heap-capture census equality tightens
   to per-iteration zero on both sides; crossing programs from
   Epics 16-18 at `SURGE_SHARDS=1,2,8` with execution witnesses;
   loop-based programs included (DEBT-040 closed).
3. **Carried Task 5 follow-ups:** cancel-after-publication-before-
   first-poll e2e (Surge-level form); lease-route caller-cancel e2e
   (`rt_far_task_release_owned` → lease → abandon, the composed
   path).
4. **Bench.** Crossing-path drop cost vs the leak model (Task 1
   recipe: worktree per commit, release build, time x5, valgrind
   totals, checksum witness).
5. **Debt closeout.** RV2-DEBT-034 closed or re-scoped with
   evidence; 047/048/040/051/052 rows finalized; Sentrux gate
   rebaseline decision; vertical 3 (owner-routed frees) scoped as
   the next epic — carries DEBT-053, the select vertical-3 deferral
   (Task 7 doc), and 054/055/056 dispositions.

## Status

IN PROGRESS (2026-07-19). Row 1 in flight.
