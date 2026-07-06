# Epic 9 Task Index

Epic 9 (`09-wakeup-and-cancellation-safety.md`) is executed as separate task
documents. Each task restates the runtime state it depends on with exact
`file:line` evidence instead of assuming the reader has the whole epic
memorized; the epic document remains authoritative for the Boundary Decisions,
Proof And Quality Contract, and Performance Contract.

This epic is proof-first. The deterministic interleaving mechanism is decided
once, up front (Task 1), and every fix task reuses it rather than re-litigating
it. Runtime-only scope: no Surge syntax/parser/semantic/lowering/stdlib-public
changes, no Phase 4 transport, no control-lane rollback of Epic 8 paths.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-proving-spike-sync-points.md` | Complete for scaffold + harness arming | proving spike | none |
| 2 | `02-debt-023-cancel-wake-token.md` | Complete for cancel-vs-park proof | runtime code | 1 |
| 3 | `03-debt-020-accept-migration-proof.md` | Complete; `RV2-DEBT-020` closed by join-route fix | runtime code + proof | 1, 2 |
| 4 | `04-debt-022-donecv-storeload.md` | Complete; `RV2-DEBT-022` closed by seq-cst external-await handshake | runtime code | 1, 2, 3 |
| 5 | `05-epic-closeout.md` | Complete; Epic 9 closed for the three owned safety debts | closeout | all |

Execution order note (architect ruling): DEBT-020 (Task 3) is pulled EARLIER
than the DEBT-022 fix so owner-replacement reachability is settled before the
completion-hot-path change lands. Suggested order is therefore
Task 1 → Task 2 (023) → Task 3 (020 proof) → Task 4 (022) → Task 5.

First-slice note (2026-07-06): Task 2's deterministic proof covers the
`RUNNING -> WAITING` cancel-vs-park window on a never-firing channel key with
external main-thread cancellation, positive `SURGE_SHARDS=1,2,8` coverage, and
an expected-failing `RV2_DEBT_023_NEGATIVE_CONTROL` run after both syncpoint
counts prove the window. Broader proof-matrix rows such as
`SP_WAKEKEY_MID_DRAIN` are not done in this slice.

## Owned Debt

- `RV2-DEBT-022`: closed by Task 4's external-await `done_cv` StoreLoad
  ordering fix.
- `RV2-DEBT-023`: closed by Task 2's cancellation vs `RUNNING -> WAITING`
  park-ordering proof.
- `RV2-DEBT-020`: closed by Task 3's join-route migration fix and
  `SP_MIGRATE_GAP` proof.
- `RV2-DEBT-003`: only if a dependency-aware completion/cancel split is taken.
  Architect ruling: the split is OUT of Epic 9 (none of the three fixes needs
  it); recommended for a follow-up epic.

## Rules

- Expand only the next task before execution; do not pre-implement later tasks.
- Every runtime-code task carries a written interleaving model (owners, locks,
  the protected transition, the old window, the new guarantee, the test that
  fails if it regresses) and runs the per-task gates recorded in
  `../09-evidence.md`.
- Durable decisions (mechanism rationale, rulings, proof outcomes) live in these
  docs and `../DEBT.md`, not in the swarm memory store (its write path is
  malformed for this session).

Task 4 note (2026-07-06): `RV2-DEBT-022` is closed by the seq-cst StoreLoad
handshake in `rt_task_await` / `mark_done` plus the post-DONE
`rt_done_cv_broadcast_after_done` helper. Positive proof covers
`SURGE_SHARDS=1,2,8`; the negative-control build strands with
`debt022 external awaiter stranded before done_cv wait`; matrix rows cover
multi-awaiters, already-DONE, parked target, and cancelled parked target.

Task 5 note (2026-07-06): Epic 9 is closed for the three owned ledger debts:
`RV2-DEBT-023`, `RV2-DEBT-020`, and `RV2-DEBT-022`. Broader cancellation matrix
rows named during planning remain optional future matrix-hardening coverage, not
new known correctness debt.
