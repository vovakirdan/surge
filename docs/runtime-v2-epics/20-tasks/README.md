# Epic 20 Task Index

Epic 20 (`20-crossing-drop-activation.md`) executes as separate task
documents; the epic document stays authoritative for the resolved
forks (per-state drop-fn registration with sorted deterministic ids,
the two-family Phase-5 seam record, DEBT-040 consumed-envelope shape,
strict-zero census, first-poll compiled-side ownership, and the
local+remote select send-arm symmetry), the acceptance bar
(dispatch-hit count AND census per abandon edge, execution witnesses
per RV2-DEBT-049), and the retry `(id=0, state=null)` rule.

Parallel lanes: Tasks 1, 2, 3 have no inbound edges and may run in
any order or interleaved with the T4→T5→T6/T7 chain. Task 7 needs
Task 3 (semantics) and Task 5 (contract). Task 8 needs all.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-epic19-closeout.md` | Complete | docs + bench: 3-tree bench (drops alone +3-11% wall, full arc net faster, exact alloc/free balance), Epic 19 CLOSED, RUNTIME_V2.md Phase 4 synced, DEBT-034 → Epic 20 | none |
| 2 | RV2-DEBT-040 CLOSED: dedicated iter-release channel (StmtIterRelease/InstrIterRelease); valgrind loop-count-independent + census gate; fixed-width residual guarded + recorded in the ledger row; DEBT-054/055 found en route | Complete | backend + census rows (parallel lane) | none |
| 3 | `03-select-send-arm-ownership.md` | Complete | sema-only fix (per-branch moves via compare machinery + SEM3140/3141); DEBT-048 closed; heap-check rows; DEBT-049 escalated + DEBT-050 opened en route | none |
| 4 | `04-drop-fn-registration.md` | Complete | backend: BodyFuncID = drop id, sorted dispatch switch → state glue, retry (0,null) kept; static IR rows; census row moved to Task 5 (RV2-DEBT-051 found: heap-bearing captures corrupt + leak, pre-existing) | none (glue shipped by Epic 19) |
| 5 | `05-handoff-contract-spawn-on.md` | Complete | DEBT-047+051 closed; PUBLICATION-ACCEPTED HANDOFF named + static contract; abandon/refusal/stale rows (2 new sync points); DEBT-052/053 opened; 2 row-4 follow-ups carried to Task 8 | 4 |
| 6 | `06-immediate-on-activation.md` | Complete | refusal/cancel-split/redelivery/anchored rows (9) on the Task 5 contract; NEW SP_IMMEDIATE_ON_BEFORE_DISPATCH window; FIXED: anchored executes joined the caller-teardown sweep (unbound-cancel contract violation, negative-control pinned); pin balance via registry reclaim rule | 5 |
| 7 | `07-remote-select-symmetry.md` | Complete | sema symmetry (far arms get SEM3140/3141 + ArmDrops); 4 race rows over Copy payloads (2 new far-select windows) pin one-lock commit atomicity, sweep refusal, cancel_routed idempotency, unpin-prefix; vertical-3 deferral recorded (abandon reconciliation + non-copy e2e ride the ChannelCreate gate opening) | 3, 5 |
| 8 | `08-bench-census-closeout.md` | Complete | DEBT-052 fixed + guards-only-borrow rule; strict census (migration = true zero; 059/060 opened); carried e2e (composed lease route); bench (drop cost ~0 wall, -44% RSS, kickoff retroactively unsound); 034 closed; vertical-3 charter recorded | all |

Rules: the established task rules apply unchanged (expand only the
next task, test-first rows, per-task gates incl. committed-tree
Sentrux comparisons, `make check` before completion, behavior-named
identifiers only, gatecheck wiring for every new tagged test).
Census rows never use string-literal payloads (literals are static)
and always assert an execution witness (RV2-DEBT-049).
