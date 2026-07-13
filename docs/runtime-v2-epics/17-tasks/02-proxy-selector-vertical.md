# Epic 17 Task 2: Proxy-Selector Runtime Vertical (matrix rows 1-14)

## Design (locked before rows)

The remote select ships as ONE anchored execute whose body runs LOCAL
select owner-side. Everything reuses the proven discipline; the reply
shape needs NO envelope extension.

### Reply shape discovery (load-bearing)

Local `select` delivers ONLY the winner index (`InstrSelect` Dst =
`select_index`; the compiled dispatch branches on it). Recv arms do NOT
deliver values into arm bodies — `rt_select_poll`'s ready probe calls
`rt_channel_try_recv_status_locked(ex, handle, NULL)` (readiness
consume, value not surfaced), and `lowerSelectAwaitExpr` extracts only
the channel from the arm's await expression. The remote reply therefore
is exactly `TaskResult<int>` with bits = winner index — identical to
the local Dst, zero new reply machinery. Matching local means matching
THIS; value-delivering select (if ever wanted) is a language change for
local and remote alike, out of scope.

### Runtime surface

- Caller: `rt_far_channel_select(const rt_far_task_handle* anchors,
  const uint8_t* kinds, const uint64_t* send_bits, uint64_t count,
  int64_t poll_fn_id, void* state, rt_remote_task_pending** pending,
  uint8_t* out_kind, uint64_t* out_bits)` — clone of
  `rt_immediate_on_execute_anchored` (pending retry snapshot,
  cancel-inflight, exactly-one reply edge). All anchors must share one
  owner shard (validated caller-side: INVALID_ARGUMENT on mix —
  dispatch never sees a mixed request; sema owns the kind diagnostic in
  Task 4). Arm arrays are copied into the pending
  (`select_arms`/`select_count`, freed with the pending).
- Op `RT_REMOTE_TASK_OP_CHANNEL_SELECT = 8`; transport kinds
  `..._FAR_CHANNEL_SELECT_REQUEST = 16` (data),
  `..._FAR_CHANNEL_SELECT_REPLY = 17` (control) — reserved in kickoff.
  Reply rides the shared `IMMEDIATE_ON_REPLY`-shaped path via
  `rt_remote_task_reply_or_finish` with the SELECT_REPLY kind.
- Dispatch (`rt_far_channel_dispatch_select`): validates the request
  match, then pins EVERY arm's channel (all-or-nothing: one stale
  anchor unpins the already-pinned prefix and answers STALE_TOKEN),
  resolves local channel pointers into the pending, creates the body
  task from the caller-provided `poll_fn_id`/`state` (compiled body in
  Task 4; harness body in this task), binds + owner-registers +
  publishes exactly like `rt_immediate_on_dispatch_execute`.
- Body helper `rt_anchored_channel_select(uint64_t* out_winner)`:
  resolves the current body's select binding (pending scan by body
  task id, like `rt_remote_task_anchored_binding_current`), builds
  handles[]/kinds[]/bits[] and calls `rt_select_poll(count, kinds,
  handles, bits, NULL, -1)`. Winner >= 0 returns; parked path yields
  (pending_key set by rt_select_poll, wait_keys registered) and the
  wake re-enters the body FROM THE TOP — `clear_wait_keys` at
  rt_select_poll's entry makes the re-poll idempotent, byte-for-byte
  the local compiled protocol. Cancel observed before poll returns
  cancelled. Timeout/default arms never reach the owner (caller-side
  fixed point).
- Loser cleanup is FREE: `mark_done` already clears `wait_keys` and
  `select_timers` for any completing task — the body task's completion
  is the loser cleanup.
- Reply-edge unpin: the select pending unpins ALL its arm channels
  where the anchored single-channel unpin runs today.

### Row-to-mode map (behavior suite, `remote_task_behavior_select.c`)

| Matrix row | Mode |
| --- | --- |
| 1 ready-before-execute | select-ready-first |
| 2 park-vs-send | select-park-then-send |
| 3 ready-vs-ready tie-break (lowest index, local scan order) | select-tie-break |
| 4 registration-vs-close (closed arm wins selection) | select-close-before |
| 5 park-vs-close wakes once | select-park-then-close |
| 6 cancel-before-owner-registration | select-cancel-unbound |
| 7 cancel-after-registration | select-cancel-parked |
| 8 cancel-vs-wake-in-flight | select-cancel-vs-send |
| 9 duplicate execute/retry | select-retry-single-body |
| 10 stale-generation wake absorbed | select-stale-wake |
| 11 lease released while parked (pins outlive leases, Epic 16 semantics: op completes, reclaim deferred to unpin) | select-release-while-parked |
| 12 sibling-lease concurrency wakes only the right selector | select-sibling-isolation |
| 13 caller teardown vs reply (orphaned reply consumed) | select-caller-teardown |
| 14 owner teardown with parked selector | select-owner-teardown |

Rows are test-first: modes land red (missing symbols/status), the
runtime lands them green, gates after each increment
(behavior suite + c-check; transport umbrella at the end).

## Execution record

All 14 rows green (commits 0cfe9c09, 82e09ea1, + races increment).
The rows caught two real defects before any compiler surface existed:
(1) a select cancel fell through to the generic handle-consuming cancel
path instead of the token-validated in-flight-execute cancel — the
pending resolved past owner-done and every arm pin leaked;
(2) `rt_immediate_on_release_owned` swept only placement executes, so a
caller cancelled before its retry poll left an in-flight select request
with no cancel route. Both fixes reuse the anchored-cancel discipline
verbatim. Harness note: census asserts after cancel rows must SETTLE
(the reply-edge unpin is asynchronous to the caller's cancel resume).

Gates at close (commit 9b19600d): behavior suite x2, transport umbrella
(make exit 0 captured directly), make check. Committed-tree Sentrux vs
the kickoff baseline (2ea291be): internal 6499->6491, runtime
5302->5297, runtime/native 5391->5384 — the usual new-subsystem
coupling for a transport vertical, inside the enforced noise-band
thresholds (RV2-DEBT-028 re-baseline discipline). File caps hold: the
select module sits beside the far-channel family, and the remote-task
pending module stays under the 300-line family gate.

### Debt position

RV2-DEBT-030 (anchored-body shape rule): the selector body is a NEW
anchored body shape — one select operation as the whole body — added
BEHIND the same first-statement discipline (re-entry from the top
replays an empty prefix). No general-body machinery is introduced;
DEBT-030 unchanged. DEBT-031/032 untouched.
