# Epic 15: Structural Cleanup And Gate Integrity

**Status:** draft for review (2026-07-12).
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

## Planned Task Slices

1. Kickoff/evidence: pin Sentrux metric semantics (`equality`,
   `redundancy`, `quality_signal` composition), measure per-metric
   commit-to-commit noise bands over recent history, and produce
   per-file / per-edge attribution for each failing scope; classify each
   contributor: real smell vs inherent coupling vs resolver noise.
   Output: a decision table (fix / rebaseline / advisory) that tasks 3-4
   execute.
1b. Liveness-panic precondition (decision 5): diagnostics capture, TSan
   stress-repetition harness, quarantine label, and a path-overlap risk
   review against every structural move planned by task 1.
2. Gate-integrity meta-test: enumerate tests vs gate patterns, wire into
   `make check`; exemption list starts empty except documented
   slow/manual suites; also assert every gate `-run` regex still matches
   at least one test (the Epic 13 "verified non-empty" practice,
   mechanized).
3. Structural pass over `runtime/native` remote-task family per the
   task-1 decision table (shared-helper extraction, module boundary
   moves, dedup of the census/debug shapes), measured on the committed
   tree after each sub-step; stop when the decision table is exhausted —
   not when the metric is satisfied.
4. Threshold re-baseline for whatever task 1 classified inherent, with
   dated rationale and margins wide enough that a small commit cannot
   flip a gate; update `SENTRUX_POLICY.md`; close or narrow
   RV2-DEBT-028/029.
5. Naming remainder: fixture-matrix promotion (C3), the 8 headers (C2),
   plan-doc closeout with a zero census.
6. Closeout: full gate set twice, Sentrux four scopes recorded, debt
   dispositions.

## Acceptance Criteria (draft)

- The three resolvable Sentrux scopes pass on the committed tree with
  noise-band-derived margins; the root scope is recorded as advisory
  with its re-promotion condition; every threshold change carries a
  dated rationale.
- The gate-integrity meta-test runs in `make check`, has caught (in a
  deliberate negative control) both rot modes: an unlisted test and a
  pattern matching nothing.
- Zero epic/task references outside `docs/` and commit messages; the
  spec row IDs resolve to a durable document.
- Every task's gates green twice; goldens comment-only.

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
