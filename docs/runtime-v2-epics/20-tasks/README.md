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
| 2 | RV2-DEBT-040: for-loop Option-box + iterator-cursor reclamation; fixed-width `Range<T>` residual verified or re-ledgered | Pending | backend + census rows (parallel lane) | none |
| 3 | `03-select-send-arm-ownership.md` | Complete | sema-only fix (per-branch moves via compare machinery + SEM3140/3141); DEBT-048 closed; heap-check rows; DEBT-049 escalated + DEBT-050 opened en route | none |
| 4 | Drop-fn registration + `__surge_drop_call` dispatch: per-state compiled drop fns, sorted deterministic ids, real-id rows, unregistered-id negative control | Pending | compiler + runtime rows | none (glue shipped by Epic 19) |
| 5 | Handoff contract + spawn-on activation: RV2-DEBT-047 fix first; named linearization point; abandon edges with dispatch-hit + census rows; three stale-generation rows | Pending | runtime + rows | 4 |
| 6 | Immediate-on activation: placement `on` + anchored `on ch` on the Task 5 contract; anchor stale/pin/unpin and reply-cancellation rows | Pending | runtime + rows | 5 |
| 7 | Remote select symmetry + abandon coverage: commit bit through reply/cancel-ack, caller-held payload obligation, cancel-vs-commit races, plumbing census | Pending | sema + backend + runtime rows | 3, 5 |
| 8 | Bench + census e2e (`SURGE_SHARDS=1,2,8`, strict-zero, execution witnesses) + debt closeout (034, 047, 048) + vertical 3 scoping | Pending | gates + closeout | all |

Rules: the established task rules apply unchanged (expand only the
next task, test-first rows, per-task gates incl. committed-tree
Sentrux comparisons, `make check` before completion, behavior-named
identifiers only, gatecheck wiring for every new tagged test).
Census rows never use string-literal payloads (literals are static)
and always assert an execution witness (RV2-DEBT-049).
