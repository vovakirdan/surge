# Epic 15 Task 1b: Liveness-Panic Precondition (RV2-DEBT-027)

**Status:** complete (2026-07-12).
**Kind:** diagnostics capture + recurrence bound + overlap review. The FIX
stays with the test-matrix epic; this task makes deferral safe while the
cleanup epic works near the panic's neighborhood.

## Diagnostics Captured

Recorded in the RV2-DEBT-027 row: panic text and site
(`task_polling_enter`, "async: double poll" guard in
`rt_async_internal.h`), original environment (WSL2 kernel, CPU count,
backend, suite-parallel context), and the absence of a focused repro
(10/10 green at count=10 historically).

## Recurrence Bound (quarantined stress target)

`make runtime-v2-liveness-stress` — manual, owner: runtime maintainers.
Composition: 50 repetitions of the park/unpark MT load
(`TestMTChannelParkUnpark`, the original failure's test) with
`SURGE_MT_TIMEOUT_SCALE=3`, plus 3 runs of the TSan completion-pin suite
— the closest existing sanitizer coverage of the polling/completion
paths the panic implicates. A dedicated channel-park TSan mode is fix-
epoch work, not precondition work. This target is the gate manifest's
SINGLE seeded exemption (task 2): quarantined, visible, owned.

Baseline datum (kickoff commit `cfca99d5`): 50/50 park/unpark repetitions
green and 3/3 TSan completion-pin suites green — zero recurrence of the
double-poll panic in this epoch's budget.

## Overlap Verdicts (moves vs the double-poll neighborhood)

The panic guards task polling state (`task_polling_enter/exit`); the
suspicious neighborhood is completion (`mark_done`), the worker turn, and
wake delivery.

| move | touches | verdict |
| --- | --- | --- |
| FC-1 (shared registry lookup) | `rt_far_channel.c` under `state->lock` only | NO OVERLAP — no polling, completion, or wake code |
| FC-2 (reclaim/reply-shape dedup) | same file, same lock, plus `rt_remote_task_reply_or_finish` CALL SITES (not its body) | NO OVERLAP with polling state; the reply path is exercised by the whole behavior suite either way |

No inventory move modifies `rt_async_internal.h`, `rt_task_complete.c`,
`rt_worker_turn.c`, or waiter stores. If task 3 discovers a move outside
this surface, the overlap review reopens before that move lands.
