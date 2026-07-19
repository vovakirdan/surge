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
- Row 2 DONE (`TestRuntimeV2CrossingStrictCensus{Balanced,ValgrindBounded}`,
  heap-check gate): the Epics 16-18 verticals under the two-tier
  discipline — in-program HeapStats windows at 1 shard, valgrind
  definitely-lost as the shard-independent witness at 1/2/8. The
  MIGRATION vertical is genuinely strict zero (flat 1 = the known
  window-edge constant) — everything this epic's drop activation
  owns reclaims exactly. Share/select verticals are NOT zero and the
  census pins their exact documented bounds instead: the bisection
  found RV2-DEBT-060 — every channel_on/share leaks its caller-side
  far-channel handle box (rt_far_channel_release exists and is never
  called from compiled code; far Task handles retire on await, far
  Channel handles have no consuming operation and no drop glue — the
  pre-arc channel model, DEBT-048's residual class, now precisely
  measured at 1,280B/52 blocks shard-independent). Fix = channel
  drop glue, vertical-3 scope, not row-sized here. Any magnitude
  change trips the pinned bounds.
- Row 4 DONE — see "Row 4 bench results" below. Headline: drop
  activation costs ~nothing on wall time (all three programs within
  noise), peak RSS drops 44% on the crossing programs and 21% on the
  local control, and the heap-bearing capture programs are EXACTLY
  alloc==free balanced at HEAD while being provably unsound at the
  kickoff tree (miscomputation + double-free even without any
  crossing — retroactive severity evidence for RV2-DEBT-051). The
  Copy-variant residual (+1 block/iteration on all three programs,
  local control included) is not crossing-attributable; candidates
  are the ledgered RV2-DEBT-057 class (the explicitly-typed
  TaskResult let is the exact union-cast confound shape).
- Row 5 DONE except one pending decision: RV2-DEBT-034 closed
  (activation delivered; owned-results tail superseded by
  RV2-DEBT-053); vertical-3 scope recorded below; the Sentrux gate
  rebaseline awaits the user's word (the baseline predates five
  epics; rules pass 10/10 throughout — the gate metric drift is
  533->571 complex functions accumulated over gated, shipped work).

## Vertical 3 scope (owner-routed frees — the next epic's charter)

Collected here per the epic's closeout duty. The vertical owns:
- RV2-DEBT-053 — owned RESULTS over the reply edge (traced design:
  populate caller_task_id for AWAIT/CANCEL + widen the teardown
  sweep, then a result_drop_fn_id threaded like the state's).
- RV2-DEBT-059 — the deferred-abandon drain for cancelled-before-run
  frames (same abandon-time machinery as 053).
- RV2-DEBT-060 — far-channel handle drop glue
  (rt_far_channel_release exists, nothing compiled calls it; the
  pre-arc channel model's first consuming operation).
- The Task 7 deferral — abandon-time commit-bit reconciliation
  (design d2) + non-copy far-channel e2e, landing together with
  opening the ChannelCreate gate to non-copy payloads.
- Dispositions to triage into it or the drop-emission family:
  RV2-DEBT-054 (while-body composite lets), 055 (for-underscore
  ICE), 056 (struct-element array boxes), 057 (union-cast source
  box), 058 (compare-arm binding obligations — unlocks the 052
  residual arm shapes).

## Row 4 bench results

Trees: kickoff `ea76cbd2` (Epic 20 opened, crossing drop-fn registration
NOT yet landed — lands at `f624c02b`) vs current HEAD `f71f7269`
(all of Epic 20 through the strict census). One git worktree per
commit under `/tmp/t8r4/{kickoff,head}`; `make build` then
`surge build --release` per program; `/usr/bin/time -f "%e %M"` x5;
valgrind totals at reduced N; a printed checksum as the execution
witness. `SURGE_SHARDS=2 SURGE_THREADS=2` throughout.

Three programs per the row spec, each a tight loop awaiting per
iteration: **A** = `spawn on distributed` (far Task, explicit await),
**B** = `on distributed` (immediate execute/reply, no separate Task),
**C** = local `spawn` + `await`, no crossing at all (isolates generic
task-churn cost from crossing cost). Each owns a per-iteration
runtime-built capture, moved by value into the crossing/spawn call.

**Soundness anomaly found first:** the heap-bearing (string-field)
capture shape is unsound at kickoff, confirming the task brief's
expectation. Native runs at N=2000 are deterministic across 5/5
tries: A and B silently miscompute (checksum flips sign — the
string-field comparison inside the crossed body reads corrupted
memory) and C hard-crashes (`free(): double free detected in tcache
2`, SIGABRT, before ever printing a checksum). Critically, **C has no
far/crossing machinery at all** — same async-task-owned-heap-capture
lowering, purely local. So the bug present at kickoff is not
crossing-specific; it is the general owned-heap-capture path, and
whatever fixed it between `ea76cbd2` and HEAD (very plausibly
RV2-DEBT-051's "own-operand UAF" fix, `5d701bae`) evidently covered
local task captures too, not only the far/crossing lowering. Under
valgrind (N=500, padded/slower allocator) the same three kickoff
programs run to completion with a *correct* checksum while valgrind
still flags 6,500 real memory errors (invalid free/read) per run —
allocator-layout-sensitive corruption, not absent corruption. Per the
task's contingency plan, an all-Copy capture variant (`Job2 { id,
mark }`, no heap field) was built for a valid cross-tree comparison;
the heap-bearing variant is reported HEAD-only plus this kickoff
anomaly record.

### Wall time / RSS, Copy-capture variant (valid cross-tree comparison), N=50,000

| Program | Tree | wall time (median of 5) | wall spread | peak RSS (median) |
| --- | --- | --- | --- | --- |
| A spawn-on | kickoff | 8.97 s | 8.81–9.03 s | 7.34 MB |
| A spawn-on | HEAD | 8.95 s | 8.79–9.14 s | 4.12 MB |
| B immediate-on | kickoff | 2.92 s | 2.91–2.95 s | 7.38 MB |
| B immediate-on | HEAD | 2.94 s | 2.94–2.98 s | 4.14 MB |
| C local control | kickoff | 0.50 s | 0.49–0.50 s | 7.41 MB |
| C local control | HEAD | 0.48 s | 0.47–0.50 s | 5.88 MB |

Checksum matched (50000) on every Copy-variant run, both trees.

### Wall time / RSS, heap-bearing (string-field) variant

| Program | Tree | N | wall time (median of 5) | peak RSS (median) | checksum |
| --- | --- | --- | --- | --- | --- |
| A spawn-on | HEAD | 50,000 | 9.09 s | 2.86 MB | correct (50000), 5/5 |
| B immediate-on | HEAD | 50,000 | 3.00 s | 2.86 MB | correct (50000), 5/5 |
| C local control | HEAD | 50,000 | 1.08 s | 4.63 MB | correct (50000), 5/5 |
| A spawn-on | kickoff | 2,000 | 0.36 s | 2.64 MB | WRONG (-2000), 5/5 |
| B immediate-on | kickoff | 2,000 | 0.12 s | 2.64 MB | WRONG (-2000), 5/5 |
| C local control | kickoff | 2,000 | 0.13 s (to abort) | ~2.64 MB | SIGABRT double-free, 5/5 |

N reduced at kickoff heap-bearing only because the corruption is
deterministic regardless of N — running longer would not produce
valid output, only a slower crash.

### Valgrind census (N=500)

| Program | Tree | variant | allocs / frees | definitely lost | errors | checksum |
| --- | --- | --- | --- | --- | --- | --- |
| A | kickoff | Copy | 9,553 / 8,029 | 16,016 B / 1,001 blk | 0 | 500 ✓ |
| A | HEAD | Copy | 9,553 / 9,031 | 8,000 B / 500 blk | 0 | 500 ✓ |
| B | kickoff | Copy | 7,555 / 6,031 | 16,016 B / 1,001 blk | 0 | 500 ✓ |
| B | HEAD | Copy | 7,553 / 7,031 | 8,000 B / 500 blk | 0 | 500 ✓ |
| C | kickoff | Copy | 7,058 / 6,036 | 16,032 B / 1,002 blk | 0 | 500 ✓ |
| C | HEAD | Copy | 7,058 / 6,538 | 8,000 B / 500 blk | 0 | 500 ✓ |
| A | kickoff | heap | 27,553 / 27,529 | 16,016 B / 1,001 blk | 6,500 | 500 (anomaly, see above) |
| A | HEAD | heap | 27,553 / 27,531 | 0 / 0 | 0 | 500 ✓ |
| B | kickoff | heap | 25,553 / 25,529 | 16,016 B / 1,001 blk | 6,500 | 500 (anomaly, see above) |
| B | HEAD | heap | 25,553 / 25,531 | 0 / 0 | 0 | 500 ✓ |
| C | kickoff | heap | 25,058 / 25,536 | 8,032 B / 502 blk | 6,500 | 500 (anomaly, see above) |
| C | HEAD | heap | 25,058 / 25,038 | 0 / 0 | 0 | 500 ✓ |

### Derived deltas

- **Drop-activation cost, wall time (Copy variant, kickoff → HEAD):**
  A −0.2%, B +0.7%, C −4%. All within run-to-run noise — activating
  crossing drops for an owned-but-Copy capture costs approximately
  nothing on wall time.
- **RSS effect (Copy variant, kickoff → HEAD):** A −44% (7.34→4.12
  MB), B −44% (7.38→4.14 MB), C −21% (7.41→5.88 MB). Largest win on
  the two crossing programs; a smaller but nonzero win on the local
  control too, so part of the RSS improvement is a general
  runtime/allocator-baseline effect, not purely a crossing-specific
  one.
- **Reclamation effect, valgrind, Copy variant:** definitely-lost
  block count exactly halves at every program (1,001→500, 1,001→500,
  1,002→500). Epic 20 activation reclaims one of the two boxes leaked
  per iteration, but a residual flat N blocks (500 at N=500) persists
  identically across ALL THREE programs, including the local-only
  control with zero crossing. That residual is therefore not a
  crossing artifact this bench can attribute to Epic 20 — it reads as
  a generic per-iteration task/compare envelope leak, consistent with
  the ledger's open RV2-DEBT-057/058 union/compare-box items.
- **Reclamation effect, valgrind, heap-bearing variant, HEAD only:**
  exact alloc == free balance (0 lost, 0 errors) on all three
  programs — full reclaim of both the envelope and the string
  payload. This is the clearest evidence of the crossing-drop
  activation's actual payoff; the Copy-only comparison above cannot
  show it because Copy captures have no payload to reclaim.

### Reading

The activation this epic shipped is cheap (near-zero wall-time
overhead on the Copy-capture control) and effective where it matters
(exact alloc/free balance on heap-bearing crossing payloads at HEAD,
vs. unsound-and-leaking at kickoff). The Copy-only cross-tree
comparison shows a real but partial win — leaked blocks per iteration
halve, not zero out — and that residual is shared by the no-crossing
local control, so it's out of this bench's attribution scope, not a
gap in Epic 20's crossing work specifically. The more consequential
finding is the kickoff-tree soundness anomaly itself: the
owned-heap-capture corruption reproduces on the local-only program
(no far/crossing path), is allocator-layout-sensitive (silent under
valgrind's padding, loud — sign-flipped or SIGABRT — under the
default allocator), and its fix window sits somewhere in the
Epic 20 range rather than being scoped to the crossing activation.
Worktrees left at `/tmp/t8r4/{kickoff,head}` for reproduction; no
tracked files touched besides this section.

## Status

ROWS 1-5 COMPLETE (2026-07-20) except the recorded Sentrux
rebaseline decision. Epic 20 closes when that word lands.
