# Epic 15: Structural Cleanup And Gate Integrity

**Status:** COMPLETE (2026-07-12). All tasks closed same-day; outcome
table and release invariants in `15-tasks/06-closeout.md`. Boundary decisions
settled by the first second-opinion pass; task slicing hardened by a full
Codex consult (17 findings, all incorporated — record below).
**Kind:** structural refactoring + tooling integrity; behavior-neutral by
contract (every change proves itself with the full gate set).

## Why This Epic Exists

Three debts converged after Epic 14:

1. **Structural metrics sit on knife edges.** Committed-tree Sentrux:
   `runtime/native` fails `min_redundancy` (0.2491 vs 0.2500) and its
   quality signal sits below the pre-remote-task baseline (5394 vs 5484,
   RV2-DEBT-028); the repository root fails `min_equality` (0.4484 vs
   0.4500, RV2-DEBT-029) and now grazes `min_redundancy` (0.7992 vs
   0.8000). The gaps are small but the direction is monotonically down
   across Epics 13-14: each transport vertical added a coupled module
   family without a compensating structural pass.
2. **Gate coverage rotted silently twice.** Two tests that would have
   caught a real regression earlier were unreachable from every
   frequently-run gate: the completion-visibility static (stale anchor
   unnoticed since the done_cv fix) and the TSan completion-pin suite
   (absent from the frequent set; it caught the blocking-pool init-order
   bug only when manually invoked). Both are wired now; nothing prevents
   the next -run list from rotting the same way.
3. **The naming cleanup has a small unexecuted remainder.** The main
   sweep landed (clusters A1/A2/B1/B2/C1 at zero); 8 golden fixtures
   still carry epic-numbered headers (C2) and the spec row IDs (C3,
   `ON-GATE-N001`…) still point into transient epic docs instead of a
   durable spec document. The plan doc still says "not started".

## Boundary Decisions

1. **Behavior-neutral contract.** No change in this epic may alter
   observable runtime or compiler behavior. Proof per task: the full gate
   set (`make check`, transport + crossing + lifecycle gates, golden
   suite) green before and after; goldens change comment-only if at all.
2. **Refactor-vs-rebaseline is decided per metric with evidence** (see
   the second-opinion record below). The default posture: fix what an
   attribution pass shows to be a real architectural smell the team
   independently recognizes; REBASELINE (threshold adjustment with a
   dated rationale in `SENTRUX_POLICY.md`) what is inherent to the
   accepted architecture. Threshold placement is noise-derived, not
   convenience-derived: measure each metric's commit-to-commit variance
   over recent history and set the threshold N noise-bands below the
   post-cleanup operating point — no knife edges, but a genuine
   regression larger than the noise band still trips the gate.
2a. **Root-scope Sentrux metrics become advisory.** The root scan cannot
   resolve 1872 of 4043 import specs (46%), so its `equality`/
   `redundancy` scores partially measure resolver behavior, not
   repository structure (the `min_equality` drift predates the last
   epic). The enforced gates are the scopes the tool resolves cleanly
   (`internal`, `runtime`, `runtime/native`); the root scan is recorded
   per closeout as informational until resolution improves — that
   condition and its rationale live in `SENTRUX_POLICY.md`.
3. **Gate integrity becomes mechanized.** A meta-test asserts every
   runtime-v2 test is reachable from a gate and every gate `-run` regex
   matches at least one test. Two hard implementation rules from review:
   the meta-test evaluates selection by INVOKING `go test -run <regex>
   -list` (never by reimplementing Go's matching semantics), and its own
   execution is itself asserted (a gate that forgets the meta-test fails
   the meta-test). Exemptions carry an owner and a reason; the direction
   of travel is package-level gating by default with explicit `-run`
   lists reserved for slow/TSan/destructive suites, and the meta-test
   guards whatever explicit lists remain.
4. **Naming remainder follows the plan's option (a).** The Epic 11
   fixture matrix is promoted into a durable
   `docs/crossing-fixture-matrix.md`; row IDs stay in fixture headers as
   pointers into it; the 8 remaining epic-numbered headers are rewritten;
   `naming-cleanup-plan.md` closes with a final census.
5. **Flake FIXES stay out; the liveness panic gets a precondition task
   IN this epic.** RV2-DEBT-018 (empty-stderr harness transient) defers
   untouched. RV2-DEBT-027 (`async: double poll` MT liveness panic) is a
   single observation of a potentially severe class — "one occurrence is
   weak evidence of frequency, not of severity" — and this epic touches
   the structures it lives near. Before any structural pass over
   completion/polling paths: capture the exact diagnostics and
   environment, add a stress-repetition harness under TSan, quarantine-
   label the row so its status is visible, and review path overlap with
   every planned move. The FIX stays with the test-matrix epic with an
   owner and exit criterion.
6. **Legacy file-size ceilings stay OUT.** `rt_term.c`/`rt_fs.c`/
   `rt_string.c` splits are real refactors of non-Runtime-V2 code with
   their own regression surface; candidate for a later epic.

## Planned Task Slices (order: 1 -> 1b -> 2 -> 3 -> 3r -> 4 -> 5 -> 6)

1. Kickoff/evidence. Pin Sentrux metric semantics, measure per-metric
   commit-to-commit noise bands over recent history, attribute each
   failing metric per file/edge ACROSS ALL THREE ENFORCED SCOPES
   (`internal`, `runtime`, `runtime/native`), and produce two
   acceptance-tested artifacts: (a) a decision table where every row
   names the metric, the evidence, the classification (real smell /
   inherent / resolver noise), the disposition (fix / rebaseline /
   advisory), the target task, the owner, and a fallback; (b) a concrete
   STRUCTURAL MOVE INVENTORY (which helpers merge, which boundaries
   move) that 1b reviews and task 3 executes. A smell found outside
   `runtime/native` gets a row and a disposition like any other — no
   scope may fail acceptance without an owned row.
1b. Liveness-panic precondition (decision 5), AFTER task 1 (it reviews
   overlap against the move inventory) and BEFORE task 2 (its stress
   harness adds a test the gate inventory must see). Concrete exit
   criteria: the exact panic diagnostics and environment fields
   recorded in the debt row; a TSan stress-repetition harness (>= 50
   iterations per run, seed/env captured) landed and quarantine-labeled
   as the exemption list's single seeded entry; a written overlap
   verdict for every move in the inventory; the handoff condition to
   the fix owner stated in RV2-DEBT-027.
2. Gate manifest + integrity meta-test. First build the CANONICAL GATE
   MANIFEST — every Makefile target and manual suite with its full
   command context (tags, env, timeout, owner, reachable-from-check or
   explicitly-manual). The meta-test then validates against that
   manifest: every runtime-v2 test selected by some gate (via `go test
   -list` UNDER THE GATE'S OWN TAGS AND ENV, never a regex
   reimplementation), every gate selecting at least one test, every
   non-manual gate reachable from `make check`. Bootstrap rule: the
   meta-test cannot prove its own execution, so `make check` invokes it
   as a direct step of the check target itself — the outer gate is the
   bootstrap. Negative controls run in isolated subprocesses and never
   flip gate status. Exemptions carry owner + reason; the list starts
   with exactly one entry (the 1b quarantined stress row).
3. Structural pass per the task-1 decision table and move inventory —
   all enforced scopes, not only `runtime/native`. Each sub-step is a
   real commit (the committed-tree measurement rule is operational, not
   aspirational). Behavior neutrality for C moves is proven beyond the
   gate set: the exported `rt_*` symbol census (`nm`) and the public
   header include graph are compared before/after each sub-step. Every
   decision-table row ends in exactly one state: FIXED (with the
   measured delta), RECLASSIFIED to task 4 (with why the refactor was
   wrong), or BLOCKED (owner + evidence). The task ends when no row is
   open — never "when the metric is satisfied".
3r. Remeasure. Noise bands and operating points are recomputed on the
   post-cleanup committed tree; task 4 consumes THESE numbers, not the
   task-1 ones.
4. Threshold re-baseline for rows classified inherent, using the 3r
   measurements, with dated rationale per threshold in
   `SENTRUX_POLICY.md`; the root scope's advisory status and its
   re-promotion condition are written there too; close or narrow
   RV2-DEBT-028/029.
5. Naming remainder. Order inside the task: inventory allowed
   references FIRST (RV2-DEBT pointers, sync-point names stay), create
   the durable `docs/crossing-fixture-matrix.md` (C3) BEFORE rewriting
   the 8 fixture headers (C2), then close `naming-cleanup-plan.md` with
   the final census against that allowlist — "zero references" means
   zero OUTSIDE the recorded allowlist.
6. Closeout. Beyond re-running per-task proofs: a clean-clone
   reproduction of the full gate set, no generated artifacts in the
   tree, Sentrux output stable across two consecutive runs, and the
   exported-symbol census unchanged relative to the epic's start except
   for moves the decision table records.

## Acceptance Criteria (draft)

- The three resolvable Sentrux scopes pass on the committed tree with
  noise-band-derived margins; the root scope is recorded as advisory
  with its re-promotion condition; every threshold change carries a
  dated rationale.
- The gate manifest exists with an owner per gate; the meta-test runs as
  a direct step of `make check` and has caught, in isolated-subprocess
  negative controls, both rot modes: an unlisted test and a pattern
  matching nothing.
- Every decision-table row is closed as fixed, rebaselined, or blocked
  with an owner — including any row outside `runtime/native`.
- Zero epic/task references outside `docs/` and commit messages; the
  spec row IDs resolve to a durable document.
- Every task's gates green twice; goldens comment-only.

## Structure Review Record (2026-07-12, Codex consult)

Seventeen findings on slicing/ordering, all incorporated: task-1 scope
widened to every enforced scope with owned rows (was: attribution
everywhere, fixes only in `runtime/native`); task 1 now outputs a
concrete move inventory (1b's overlap review had an impossible
dependency on a plan that didn't exist); order fixed to 1 -> 1b -> 2 (the
stress harness adds a test the gate inventory must see); the
quarantine-vs-empty-exemptions contradiction resolved (one seeded owned
entry); the meta-test's own execution bootstrapped by the `make check`
target directly; regex matching replaced by manifest-driven `go test
-list` under each gate's tags/env; task 3 given a per-row failure
disposition and ABI/symbol/include-graph neutrality checks; an explicit
remeasure step (3r) inserted so task 4 does not encode stale noise
bands; sub-step measurement defined as real commits; task 5 ordered
(allowlist -> C3 doc -> C2 headers) with "zero references" scoped to the
allowlist; 1b given numeric exit criteria; closeout given release
invariants (clean clone, artifact-free tree, stable Sentrux, symbol
census).

## Second-Opinion Record (2026-07-12, Codex via /second-opinion)

Verdicts: decision 1 sound-with-changes (margin rule reformulated to
noise-bands; "wide enough that small commits can't flip it" rejected as
arbitrary and gate-neutering; layered scheme noted: hard floor + delta
signal + trend review, ratchet as review signal only); root-scope
advisory downgrade was the reviewer's highest-confidence recommendation
(46% unresolved imports = gating on resolver noise). Decision 2
sound-with-changes (invert the default to package-level gating as the
destination; meta-test evaluates selection via `go test -list`, never a
regex reimplementation; exemptions owned; meta-test's own execution
asserted). Decision 3 sound-with-changes (legacy-file exclusion correct;
empty-stderr deferral fine if tracked; investigation-free deferral of the
double-poll panic rejected — the precondition task above is the mandated
middle ground). All incorporated.

## Stop Conditions

- If task-1 attribution shows a metric is dominated by resolver noise
  (e.g. the root scan's 1872 unresolved import specs) rather than code
  structure, stop refactoring against it and route the finding to the
  rebaseline task.
- If a structural move would change behavior to satisfy a metric, the
  move is out — record and rebaseline instead.
