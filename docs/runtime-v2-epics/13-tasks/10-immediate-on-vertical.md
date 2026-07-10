# Epic 13 Task 10: Immediate `on` Executable Vertical

**Status:** complete (2026-07-10). Evidence: `NOTES.md` "Epic 13 Task 10
Complete"; capability open in `internal/buildpipeline/crossing_transport.go`;
acceptance rows wired into `make runtime-v2-transport-contract-check`.
**Kind:** LLVM lowering + runtime e2e. Dedicated execute/reply message
category — the spawn+await desugar is rejected by the epic's Lowering
Contract.
**Depends on:** Task 6, Task 7 (representation), Task 9 (cancel routing
precedent and matrix state).

## Goal

Lower `on shard(id) { ret value; }` and `on distributed { ret value; }` to
the dedicated immediate execute/reply category: one request, one reply, one
cancellation token, no publicly observable `far Task<T>` handle — and prove
`TaskResult<T>` semantics, cancellation, and trace equivalence end to end.

## Why Dedicated (bind the epic decision in code review)

The desugar's transient handle between publication ack and await is a new
cancellation/cleanup seam present in neither pure spawn nor pure await, pays
a second round-trip on the hottest immediate path, and cannot produce
equivalent trace evidence. Any attempt to implement this task as a desugar
is a design change and must go back for review with the written proof the
epic demands.

## Starting State (verify and re-pin)

- Spine categories: immediate execute request / reply declared in Task 4's
  envelope; unimplemented until here.
- Task 9's owner-routed cancel and token discipline — the immediate form
  reuses the token concept for its single reply edge (request id +
  generation; exactly one of {reply, cancel-ack} resumes the caller).
- Sema: `on placement` records `CrossingLoweringOnPlacement` with
  destination/captures/result type; `on far_handle` is NOT in this epic
  (stays FUT7014).
- Epic 11 semantics: `on` returns `TaskResult<T>`; `ret` is block-local;
  suspension-legal contexts only.

## Scope

In: execute/reply message implementation on the spine (request carries
captures + body per the payload decision; destination runs the body as a
task on its shard; reply carries `TaskResult<T>`), LLVM codegen (suspend on
reply), caller-cancellation routing for an in-flight execute, e2e tests,
capability flip for `on shard(id)` / `on distributed` on LLVM, guard-matrix
update.

Out: `on far_handle` (remains guarded), `on pool` (placement diagnostic),
desugar implementation, VM.

## Steps

1. **Test-first** rows (`SURGE_SHARDS=1,2,8`):
   - `on shard(k)` returns `TaskResult<T>` with the body's value, body ran
     on shard k (owner proof);
   - `on distributed`: policy + non-caller proof at shards>1;
   - self-crossing (`shard(current)`, shards=1): completes without deadlock
     — the caller's task suspends, the shard drains its own queue and runs
     the body (THE N=1 forcing-function row);
   - caller cancelled while suspended on the reply: exactly-one resume
     (reply vs cancel path) via sync-point interleavings; destination-side
     body observes cancellation per the decided semantics; no strand, no
     leak;
   - destination shutdown mid-execute: deterministic `TaskResult`/error per
     the decided mapping;
   - trace equivalence: counters show ONE request + ONE reply per `on` (no
     publication-ack pair) — the dedicated-category proof;
   - no-hidden-fallback: capability OFF -> FUT7014 unchanged; `on
     far_handle` and `on pool` rows still produce their diagnostics after
     the flip.
2. Implement the execute/reply handlers on the spine.
3. Implement codegen: captures, placement resolve, execute request, suspend,
   `TaskResult<T>` materialization from the reply.
4. Wire caller-cancel routing for in-flight executes.
5. Flip `backendSupportsCrossingForm(LLVM, on_placement)`; update the
   crossing-guard matrix rows deliberately.

## Proof

- All Step 1 rows green; race rows via sync points.
- Post-flip `make runtime-v2-crossing-check` twice; compile-only negative
  space clean; `on far_handle` / `pool` / VM / unknown-backend rows still
  deterministic.
- `make c-check`, `make cppcheck`, `make golden-check`,
  `./check_file_sizes.sh -a`, Sentrux scoped scans, `make check`.

## Stop Conditions

- Cancellation of an in-flight execute cannot be expressed without the
  distributed-scope protocol — stop; the single request-scoped token must
  suffice, else design review.
- Trace equivalence cannot be met by the dedicated category either — the
  Lowering Contract row itself is wrong; stop and re-open it with evidence.
