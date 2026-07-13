# Epic 17 Task 1: Kickoff — Evidence, Contract Clause, Sema Surface, Reservations

Direction approved 2026-07-13 (Model C per the epic doc's fork
resolution; the four review points — B-over-A tail, owner-lane
linearization clause, single-owner sema restriction, in-epic
stabilization slice — were confirmed explicitly).

## Evidence Re-Pin (verified at commit `2ea291be`)

Local select machinery (the contract to match, reused wholesale
owner-side):

- `rt_select_poll(count, kinds, handles, values, ms, default_index)` —
  `runtime/native/rt_async_select.c:238`; task-only variant
  `rt_select_poll_tasks` at `:142`.
- Per-task multi-key registration: `wait_keys` (registration list) and
  `select_timers` (timeout arms) on `rt_task`
  (`rt_async_internal.h`); `ensure_select_timers_cap`
  (`rt_async_select.c:16`) owns timer growth. `mark_done` clears both
  (`rt_task_complete.c`: `clear_wait_keys`, `clear_select_timers`).
- MIR: `InstrSelect` with `SelectArmKind`
  {Task, ChanRecv, ChanSend, Timeout, Default}
  (`internal/mir/instr.go:319-350`); lowering in
  `internal/mir/lower_expr_select.go:20` (`lowerSelectExpr`).
- Loser-cleanup rows already owned by local select:
  timeout-cleans-losing-waiter, cancelled-select-cleans-keys
  (`rt_async_select.c` row suite).

Anchored execute/reply discipline (the transport shape the proxy
selector ships on):

- Anchored op template: `share()` — caller side
  `rt_far_channel_share`, dispatch `rt_far_channel_dispatch_share`
  (`runtime/native/rt_far_channel.c`); pending retry via ptr-null
  source on re-entry (`internal/backend/llvm/emit_crossing_share.go`).
- Exactly-one reply edge, cancel-inflight, orphaned-reply autonomous
  consumption: row-proven in Epics 13-16
  (`rt_remote_task_dispatch.c` / `rt_remote_task_completion.c`).
- Anchored-body park protocol precedent:
  `rt_anchored_channel_send/recv/close` (yield inside, re-entry from
  top) — the proxy selector's parked shape follows it.

Detector surfaces (Task 3's ground):

- Suspect scan: `find_channel_parked_body`
  (`runtime/native/rt_remote_task_deadlock.c:125`), lease-topology
  wording via `rt_far_channel_active_lease_count` (`:233`).
- The new wait chain (caller selector parked -> remote select op ->
  owner-side channel waiter) must collapse into ONE logical selector
  op in that scan (epic doc, detector requirement).

Sibling leases (source-level fan-in legality): `share()` shipped in
Epic 16; `rt_far_channel.c` lease table (generation = lease identity).

## Code-Space Reservations

- Transport kinds: `RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST = 16`
  (data lane), `RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY = 17`
  (control lane) — next free pair after SHARE 14/15
  (`rt_transport.h:36`).
- Remote-task op: `RT_REMOTE_TASK_OP_CHANNEL_SELECT = 8` — next free
  after CHANNEL_SHARE 7 (`rt_remote_task_internal.h:17`).
- Sema: `SemaSelectFarArmsSingleOwner Code = 3176` — next free after
  `SemaOnChannelOp` 3175 (`internal/diag/codes_crossing.go:112`).
- Guard: `FutChannelSelectBackendUnavailable Code = 7022` — next free
  after `FutChannelShareBackendUnavailable` 7021 (`:159`).
- Crossing lowering kind: `CrossingLoweringChannelSelect` appended at
  the enum END (`internal/sema/crossing_lowering.go`; mid-insert
  renumbers — Epic 16 gotcha).

## Contract Clause (landed in `docs/RUNTIME_V2.md` with this task)

Remote `select` linearizes on the owner lane: the winner is decided on
the channel owner's shard exactly where the owner's own local `select`
would decide it. No caller-lane ordering or fairness is promised across
the shard boundary — two callers selecting over the same channels
observe an owner-lane order, not their submission order. This writes
down what was always true of the execute/reply discipline; no existing
promise is narrowed (verified: the contract doc frames remote select
purely as a cost/predictability concern, never as a fairness one).

## Sema Surface Design (implemented in Task 4)

Vertical-1 shape rule, kindness-first:

- A `select` whose arms include ANY far-channel operation is a remote
  select. In vertical 1 every far arm must target channels sharing ONE
  owner shard, and local channel arms / task arms may not mix with far
  arms. Timeout and default arms stay caller-side and are always
  allowed (fixed point).
- `SemaSelectFarArmsSingleOwner` (SEM3176) diagnoses violations at the
  offending arm's span, names the restriction, and gives the
  workaround: split into per-owner selects (mirroring the SEM3175
  anchored-body shape-rule template: name the real cause at sema,
  offer the rewrite).
- Owner-shard sameness is decided by the same sema evidence that
  routes `channel_on`/`share()` results (far-typed channel values
  carry their placement through `CrossingLoweringInfo`); arms whose
  owner cannot be proven equal at compile time are diagnosed with the
  split-into-selects hint, not deferred to runtime.
- Off-transport backends get `FutChannelSelectBackendUnavailable`
  (FUT7022) through the existing classifier
  (`internal/buildpipeline/crossing_guard_classify.go`) with the
  sync-context (FUT7019) and payload (FUT7020) causes ranked first,
  same as every crossing form.

## Baselines (Sentrux, committed tree `2ea291be`)

| Scope | Quality |
| --- | --- |
| `.` (root, advisory) | 6178 |
| `internal` | 6499 |
| `runtime` | 5302 |
| `runtime/native` | 5391 |

Note: `runtime/native` moved 5405 -> 5391 across the RV2-DEBT-027 fix
(site-coded panic + sync point + locked re-validation) — new coupling
from the fix vertical, within the enforced noise-band gates (`make
check` green at `2ea291be`). 5391 is the epic-17 starting operating
point.

## Open Debt Touched

None of RV2-DEBT-030/031/032 is touched by this task. Task 2 must
state its position against DEBT-030 (anchored-body shape rule) because
the proxy selector introduces a new anchored body shape (a select as
the body's single operation).
