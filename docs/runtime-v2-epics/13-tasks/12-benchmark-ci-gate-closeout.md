# Epic 13 Task 12: Benchmark, CI Gate, And Closeout

**Status:** pending.
**Kind:** bench + CI + docs/debt closeout.
**Depends on:** all previous tasks.

## Goal

Promote the stable transport gate into CI, produce the honest first
performance evidence, close or reassign every debt this epic touched, update
the docs, and write the handoff so the next epics (remote channels, remote
`select`, Tier 2 pool, distributed scopes) reuse the spine instead of
inventing parallel wake/message paths.

## Gate

- New focused target `runtime-v2-transport-check` (Makefile), covering: the
  Task 3 park/wake matrix, Task 6 publication rows, Task 8-10 e2e verticals,
  the Task 9 race rows, and the Task 11 matrix — with exact `-run` regexes
  that match real test names (no dead regex; verify each matches at least
  one test, the Epic 12 closeout rule).
- Stability bar before wiring: the gate passes twice consecutively, on the
  Task 2-hardened harness, at `SURGE_SHARDS=1,2,8` rows included.
- Wire into `runtime-v2-check` (Makefile:86-108 block) only after the
  stability bar; `runtime-v2-crossing-check` stays green with its updated
  expected rows.

## Benchmark

- One native crossing throughput/latency row under `SURGE_SHARDS=1,2,8`
  (e.g. N `spawn on` + await round trips per second, and immediate `on`
  round-trip latency distribution), calibrated honestly: this proves
  correctness and liveness cost, not final line-rate scaling.
- Tooling rule (`RV2-DEBT-006` lesson): the script owns per-probe timeouts
  and reports probe/mode on timeout — follow `scripts/stallrepro.py` /
  `run_stallrepro.sh` shape, do NOT copy `bench_native_channels.sh`.
- Record the numbers in the evidence file as the baseline for the
  remote-channel epic.

## Trace Counter Review

Verify the epic's required counters exist and are exercised by at least one
test: transport enqueue, wake writes, wake elision, credit stalls (declared;
may be structurally zero until the data lane exists — record that),
completion replies, cancellation replies, stale generation-token drops,
unsupported fallback attempts (must be zero everywhere; a nonzero value is a
bug by definition — consider asserting that in the gate).

## Quality Closeout

- `make check`, `make c-check`, `make cppcheck`,
  `./check_file_sizes.sh -a` (any touched `RV2-DEBT-005` allowlisted file:
  record shrank/flat/follow-up owner).
- Sentrux: root + `runtime/`, `runtime/native/`, `internal/` scans per
  `SENTRUX_POLICY.md`; compare quality numbers against the Task 1 baseline
  and record deltas.
- `make golden-check`.
- Run `make runtime-v2-check` twice consecutively.

## Debt Closeout

- `RV2-DEBT-011`/`018`: record the Task 2 narrow-close evidence and what
  remains for Backend/Test Matrix Cleanup.
- `RV2-DEBT-024`: record the Task 1 decision outcome with its test
  reference; either partially closed for direct lowering or reaffirmed with
  the exact higher-order boundary.
- `RV2-DEBT-025`/`026`: owners reassigned per Task 1; affinity
  reaffirmation (if that was the outcome) noted as a transport invariant.
- Any new debt discovered by Tasks 3-11 has a row with an owner before
  closeout.

## Docs

- Epic document: Closeout section (final shape, acceptance evidence per
  criterion, debt disposition, handoff) in the Epic 12 closeout style.
- `NOTES.md`: handoff entry.
- `DEBT.md`: rows updated with dated evidence.
- `README.md` (runtime-v2-epics): epic status + the new gate.
- `docs/RUNTIME_V2.md`: mark which Phase 4 items are now executable
  (placement task crossing on LLVM/native) and which remain future work —
  do not overstate; remote channels/select/scopes/migration/credits remain
  open.

## Handoff To The Next Epics

Answer, with pointers into code/tests (the epic's Handoff contract):

- what runtime message each remaining Phase 4 form needs (remote channel
  send/recv, remote `select`, distributed scope cancel/complete) and which
  spine categories they reuse;
- where credits/data-lane accounting resumes (the non-promoted spike record);
- which compile-time metadata the next lowering consumers can rely on
  (per-form HIR/MIR nodes + capability predicate);
- which tests fail today ONLY because those features are intentionally
  unavailable;
- the benchmark baseline numbers.

## Acceptance

This task is complete when every Acceptance Criteria bullet in the epic
document has a named piece of evidence (test, command output, or document),
recorded in this file or the epic's Closeout section.
