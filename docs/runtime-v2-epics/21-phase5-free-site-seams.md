# Epic 21, Task 9 — the Phase-5 free-site seam inventory

Written 2026-09-03 (Wave F, F3), from the tree at `799d2f75`. Epic 20's
crossing-drop activation (`20-crossing-drop-activation.md`, the two families
it names) asked for this inventory and it was never produced; Epic 21 Task 9
lists it as missing evidence (RV2-DEBT-125). It is a map of WHERE a crossing's
bytes change owner and WHERE they are finally freed, one line per site, so an
allocator Phase 5 (a plan-limited or transport-owned buffer, which the
2026-08-29 ruling says does not exist yet) knows every seam it would have to
cut. Nothing here is new behaviour; every line is a reading of the code.

The resident-byte telemetry of Wave E's E5 (`rt_resident_bytes.h`) charges
exactly these seams -- ENVELOPE/PADDING at the lane, RECORD at the pending,
PAYLOAD at the state block and the staged arm cells, SIDECAR at the arm
table -- so the inventory and the ledger are the same list read two ways.

## Family 1 — obligation-transfer sites (the owner changes, no bytes move)

| what | from → to | site | the transfer |
| --- | --- | --- | --- |
| shipped state block | caller → pending | `rt_remote_spawn.c` (spawn-on), `rt_immediate_on.c`, `rt_immediate_on_anchored.c`, `rt_far_channel_select.c` (submission) | `state_owned = 1`, PAYLOAD charged |
| shipped state block | pending → body task | the PUBLICATION-ACCEPTED HANDOFF in the same four files | `state_owned = 0` under the dispatch's own reference (contract: `rt_remote_spawn_internal.h`), PAYLOAD released; from here the block is the body's frame |
| SEND arm payload | caller storage → arm cell | `rt_far_channel_select.c`, staging loop | `rt_value_move_init_detached` into the cell, `rt_value_cell_commit`; a wide payload's block charged at the bind |
| SEND arm payload (winner) | arm cell → channel | `rt_anchored_channel_select` after `rt_select_poll` | the local select moves out of the cell's own storage; the cell is marked MOVED so no later reader destroys it |
| SEND arm payload (losers) | arm cell → caller storage | `select_return_arms` on a success reply | moved back on the caller's terminal retry, cell marked MOVED |
| task result | producer slot → caller | `rt_remote_task_result.c` (`take_result_source`, the far-carried adopt path, the local `RT_TASK_TAKE_MOVE`) | moved out of the producer's `rt_value_cell` under the lease; the far reply NAMES the slot (`rt_result_source`) and pins the producer until the take |
| task result block | task cell → taker | `rt_value_cell_hand_off` (`rt_value_cell.c`), used by `rt_task_lifetime.c` | the block leaves the cell (`owns_block = 0`); the taker frees it through `rt_value_release_owned_block` |
| pending record | dispatch → owner registration → reply message | `rt_remote_task_pending_register_owner`, `enqueue_reply` (`rt_remote_task_dispatch.c`) | the in-flight reference rides the registration, then the reply envelope's `payload`; consumed by `dispatch_reply` |
| far channel handle | caller field → drop glue | `emit_drop_glue.go` (E3) | `rt_far_channel_handle_drop`; an `on far_handle` anchor is a LEASE (`CrossingCaptureAnchorLease`), never a move |

## Family 2 — actual free sites (the bytes go back)

| what | freed by | site | width |
| --- | --- | --- | --- |
| envelope | pop (or lane destroy at teardown) | `rt_transport_pop_locked`, `rt_transport_state_destroy` (`rt_transport.c`) | ring slot reuse, `sizeof(rt_transport_msg)` charged/released |
| remote-task pending | last reference | `rt_remote_task_pending_release` (`rt_remote_task_pending.c`) | `sizeof(rt_remote_task_pending)` |
| spawn pending | last reference | `remote_spawn_pending_release` (`rt_remote_spawn_pending.c`) | `sizeof(rt_remote_spawn_pending)` |
| shipped state block, never handed off | pending's last release | the two releases above, `rt_value_release_owned_block` gated on `state_owned` | descriptor `layout.size` |
| shipped state block, pre-submission refusal | the refusing call | `*_drop_unshipped_state` in the four submission files | descriptor `layout.size` |
| shipped state block, handed off | the body | compiled code at the body's return (Epic 24 step 0) | descriptor `layout.size` |
| arm table and its cells | the one free site | `rt_remote_task_select_arms_free` (`rt_remote_task_pending.c`), from the pending's last release or the pre-pending failure path | `count × sizeof(rt_far_channel_select_arm)`, each cell disposed (`rt_value_cell_dispose`, a MOVED cell destroys nothing) |
| task result cell block | dispose or the taker | `rt_value_cell_dispose` (`rt_value_cell.c`), `rt_value_release_owned_block` after a hand-off | descriptor `layout.size`; inline results (≤ 16 bytes) free nothing |
| task | last handle reference on a completed task | `rt_task_lifetime.c` | `sizeof(rt_task)` (tasks come from the segmented table; the segment is never freed) |
| blocking job state | the last of the two references (awaiter, pool) | `blocking_job_release` (`rt_async_blocking.c`), through the adopted `rt_value_cell` | descriptor `layout.size` |

## What Phase 5 would cut, if it existed

A transport-owned buffer changes exactly two lines of family 1 -- the state
block would be COPIED into the buffer at submission and the buffer, not the
block, handed off -- and adds one line to family 2 (the buffer's free at pop
or handoff). Every other seam is unaffected, which is why the 2026-08-29
ruling could withdraw the byte credit without touching them: the slot budget
bounds envelopes, and envelopes own no bytes.
