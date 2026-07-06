# Epic 9 Task 5: Epic Closeout

Closeout task for Epic 9 (Wakeup And Cancellation Safety). This document
consolidates the deterministic proof results, reconciles the debt ledger, and
records the final handoff before the next runtime planning pass.

Baseline for anchors: Epic 9 opened at `d80ef41c`. The final code slice before
this closeout is `82c633a7` (`fix(runtime): close external await done_cv race`).

## Task Ledger

| Task | Commit(s) | Result |
| --- | --- | --- |
| 1 proving spike | `dfbf5897` | Test-only sync-point scaffold added, statically allowlisted, and wired into `runtime-v2-check` through `runtime-v2-syncpoint-check`. |
| 2 `RV2-DEBT-023` cancel wake token | `dfbf5897` | Closed the cancel-vs-`RUNNING -> WAITING` park window with an unconditional wake token and positive/negative sync-point proof. |
| 3 `RV2-DEBT-020` join-waiter migration | `ff57b8a2` | Closed the owner-replacement join-waiter migration gap with an atomic join-owner route and `SP_MIGRATE_GAP` proof. |
| 4 `RV2-DEBT-022` external await `done_cv` | `82c633a7` | Closed the external-await StoreLoad lost-wakeup window with seq-cst helper pairs and a guarded post-`DONE` `done_cv` broadcast helper. |
| 5 closeout | this commit | Contract sweep, status reconciliation, RUNTIME_V2 flowback, and next-runtime handoff. |

## Acceptance Verification

| Epic acceptance clause | Verdict | Evidence |
| --- | --- | --- |
| `RV2-DEBT-022` closed with deterministic proof and ordering argument | MET | Task 4, `09-evidence.md` Slice 4, `TestRuntimeV2LifecycleDebt022DoneCVStoreLoadProof`, negative-control proof, and external-await matrix. |
| `RV2-DEBT-023` closed with deterministic proof and ordering argument | MET | Task 2, `09-evidence.md` Slice 2, `TestRuntimeV2LifecycleDebt023CancelParkWakeTokenProof`, and negative-control proof. |
| `RV2-DEBT-020` closed by deterministic migration-gap proof | MET | Task 3, `09-evidence.md` Slice 3, `SP_MIGRATE_GAP`, positive proof at `SURGE_SHARDS=2,8`, and negative-control proof. |
| Changed wake/cancel/await paths have ownership and cleanup invariants | MET | Task 2 wake-token rule in `cancel_task`, Task 3 join-route protocol, Task 4 seq-cst external-await helper cluster and `rt_done_cv.c` helper. |
| Runtime/code gates pass or have recorded non-Epic blockers | MET | Final code slice ran `git diff --check`, `make c-check`, `make cppcheck`, `make runtime-v2-syncpoint-check`, `make runtime-v2-lifecycle-check`, `make runtime-v2-perf-check`, `make runtime-v2-check`, `make check`, LOC, and Sentrux scans. |
| Docs/debt/status updated | MET by this closeout | `DEBT.md`, `NOTES.md`, this task doc, the epic doc, the epics README, `09-evidence.md`, and `docs/RUNTIME_V2.md`. |

## Proof Coverage Disposition

Epic 9 used deterministic sync points for the three known instruction-scale
windows instead of relying on stress. The final state is:

| Area | Disposition |
| --- | --- |
| External await vs running target | Covered by `Debt022DoneCVStoreLoadProof` with `SP_AWAIT_AFTER_INCREMENT`, `SP_AWAIT_BEFORE_DONECV_WAIT`, and `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD`. |
| External await vs parked and already-DONE targets | Covered by `Debt022ExternalAwaitMatrix`. |
| Multiple external awaiters | Covered by `Debt022ExternalAwaitMatrix`; the completion helper broadcasts, not signals. |
| Cancel vs `RUNNING -> WAITING` park on a never-firing key | Covered by `Debt023CancelParkWakeTokenProof` at `SURGE_SHARDS=1,2,8`; negative control proves the old status-gated wake strands. |
| `RV2-DEBT-022` x `RV2-DEBT-023` intersection | Covered by the cancelled parked target row in `Debt022ExternalAwaitMatrix`. |
| Accept-transition owner replacement | Covered by `Debt020MigrateGapProof` and `Debt020MigrateGapNegativeControl`; production now uses the join-route protocol for all `WAKER_JOIN` add/remove/pop/collect paths. |
| Shutdown after wake/cancel changes | Covered by the promoted lifecycle and net shutdown gates inside `runtime-v2-check`. |
| TSan/stress corroboration | The final lifecycle gate includes the existing completion-pin TSan oracle. The Epic 9 races themselves are proven by positive/negative deterministic sync points, which are the load-bearing proof. |

The original proof list also named broader cancellation-matrix rows such as a
sleep-specific cancel row and a cancel-while-`wake_key_all`-mid-drain row. Epic 9
does not pretend those have dedicated new deterministic rows. They are not
known open correctness bugs after the unconditional wake-token fix, and they are
not separate debt items in `DEBT.md`; they remain candidates for a future
test-matrix or safety-hardening epic if we decide to broaden cancellation
coverage beyond the three ledger debts closed here.

## Debt Reconciliation

| ID | Final state |
| --- | --- |
| `RV2-DEBT-020` | CLOSED by Task 3. The generic join-route protocol removes the stale old-owner registration gap. |
| `RV2-DEBT-022` | CLOSED by Task 4. External-await and completion now share one seq-cst StoreLoad handshake; the only completion `done_cv` broadcast helper is external-await compatibility and is statically pinned. |
| `RV2-DEBT-023` | CLOSED by Task 2. `cancel_task` always sets a wake token through the owner shard; READY/RUNNING/WAITING/DONE no-resurrection reasoning is recorded in the task doc and code comment. |
| `RV2-DEBT-003` | OPEN. The completion/cancel split was deliberately not taken in Epic 9 because none of the three safety fixes required it. Task 4 removed the old `done_cv` filename blocker by moving the broadcast helper into `rt_done_cv.c`. Sentrux gate degradation remains the existing cumulative coupling/complexity recovery class under this debt. |
| `RV2-DEBT-017` | OPEN and untouched. Sync-channel compatibility latency is outside Epic 9. |
| `RV2-DEBT-001`, `002`, `011`, `018` | OPEN and unchanged. These stay with the later VM/native/LLVM test-matrix and harness hardening work. |
| `RV2-DEBT-005`, `006`, `007`, `010`, `012`, `013` | OPEN and unchanged. These stay with their owning cleanup, benchmark, quality, net-handle, heap, and stdlib/server work. |

No new Epic 9 debt is opened by this closeout.

## Final Gates

The final code slice (`82c633a7`) recorded the load-bearing gates:

- `make runtime-v2-check`: pass.
- `make check`: pass.
- `make c-check`: pass.
- `make cppcheck`: pass.
- `make runtime-v2-syncpoint-check`: pass with seven Epic 9 sync-point
  enumerators.
- `./check_file_sizes.sh -a`: pass; key touched files were
  `rt_async_state.c` 1183, `rt_async_internal.h` 630, `rt_async_task.c` 317,
  `rt_done_cv.c` 20, `rt_sync_point.c` 182, `rt_sync_point.h` 39.
- Sentrux `check`: root `6177`, `runtime` `5327`, `runtime/native` `5430`, all
  pass.
- Sentrux `gate`: still reports the existing cumulative `RV2-DEBT-003`
  recovery class (complex-function/coupling drift) while runtime/native quality
  is above the stored baseline (`5159 -> 5430`).

Final performance counters from `make runtime-v2-check`:

- `control_lock_acquired=11819` (`11.542/req`);
- `ctrl_await_compat=3458` (`3.377/req`);
- steady-state-control `8361` (`8.165/req`, ceiling `20.0`);
- lifecycle-control `6143` (`5.999/req`, ceiling `9.0`);
- `placement_adoptions=253`;
- `accept_owner_active_shards=8`.

Epic 9 was not expected to improve throughput. Its value is that three
previously named ordering assumptions are now deterministic proofs with
negative controls.

## Next Runtime Handoff

The next runtime planning pass can proceed with the local wakeup,
cancellation, and accept-transition debts closed. The natural next choices are:

- dependency-aware cleanup that reduces `RV2-DEBT-003` instead of only moving
  lines;
- net-handle/stdlib owner-safety work (`RV2-DEBT-010` / `RV2-DEBT-013`);
- benchmark/test harness hardening before the final VM/native/LLVM matrix;
- the explicit Phase 4 crossing surface, but only after the mandatory language
  syntax review with the user.

No Epic 9 task changed Surge syntax, keywords, parser rules, semantic analysis,
lowering, public examples, Phase 4 inbound messaging, eventfd credits, remote
`select`, or shard-movable checking.
