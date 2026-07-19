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

## Progress

- Row 1 DONE (RV2-DEBT-052 CLOSED — see the ledger row for the full
  record). Two rounds: the fix (per-arm scrutinee release in the
  normalize pass, the DEBT-040 channel renamed to the shared
  `EnvelopeRelease`), then the adversarial-review blocker — a
  PRE-EXISTING guard-fallthrough UAF (extraction precedes the guard;
  a by-value consuming guard freed the payload later arms re-extract)
  that the fix's deep-drop would have turned into a double-free.
  Root-cause remedy: guards only borrow — moving a current-arm
  pattern binding inside its guard is rejected
  (`SemaCompareGuardMovesBinding`, kindness-first wording; LANGUAGE.md
  updated). Census: 6-probe strict-zero row incl. the previously
  undodgeable extraction/borrow-guard-fail/deep-drop path; the
  crossing census tightened to independent d8==d1 and b8==b1;
  valgrind control 32B/2 blocks -> 0/0 (retroactively re-attributing
  the two 16B blocks the DEBT-051/053 investigations called entry
  states — they were TaskResult scrutinee boxes). Await-in-arm MIR
  golden pins the release riding post-resume with the scrutinee
  persisted across suspension. Found en route: RV2-DEBT-057
  (union-cast source box leak), RV2-DEBT-058 (compare-arm payload
  bindings never get scope-exit drops — also the recorded reason the
  mixed-payload and whole-value-binding arm shapes keep pre-fix
  leak-over-wrong-free behavior).
- Row 3 DONE (`TestRuntimeV2FarTaskCallerCancel`, transport gate):
  both carried Task-5 follow-ups as Surge-level e2e over the COMPOSED
  caller-cancel route (local `Task.cancel()` on the awaiting caller →
  `rt_far_task_release_owned` → lease route → abandon — the path the
  direct-abandon harness rows bypassed). The
  cancel-after-publication-before-first-poll window is reliably hit
  at 2+ shards with zero yields (15/15); at 1 shard the window is
  sub-scheduler-tick (one yield already completes the whole round
  trip synchronously) — asserted as the shard-dependent split rather
  than forced. Phase sweep (0/1/6 yields x3): the far body runs in
  all 9 invocations at every shard count — in-flight semantics hold
  regardless of cancel timing. Valgrind census pins SAFE-AND-BOUNDED
  (no crash-class errors; loss a clean multiple of the
  per-occurrence unit) instead of zero, because the e2e FOUND
  RV2-DEBT-059: any task cancelled before its body ran leaks its
  init state+payload pair (minimal local repro, no far machinery) —
  the deferred-abandon path never drains. First Go test in the repo
  to drive valgrind directly (self-skips without it).
- Rows 2, 4, 5 pending.

## Status

IN PROGRESS (2026-07-19). Row 1 landed.
