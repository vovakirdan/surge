# Epic 7 Task 14: Large-File And LOC Tranche

**Kind:** hygiene. **Depends on:** Task 11.

**Goal:** verify the epic's extraction boundaries, keep every touched file
inside the size gate, and tighten `.loc-legacy-allowlist` where the epic
reduced files.

## Scope

- Extractions this epic (all inside the gate, effective LOC):
  `rt_lane.c`, `rt_waiter.h`, `rt_waiter_route.c`, `rt_sched_wake.c`,
  `rt_async_deque.c`, `rt_async_select.c`, `rt_async_sleep.c`,
  `rt_worker_turn.c`, `rt_net_poll_pass.c`, and the Task 11 B2 channel
  split (`rt_async_channel.c` 276, `rt_channel_lane.h` 210,
  `rt_channel_sync.c` 290).
- `.loc-legacy-allowlist` tightened:
  - `rt_async_state.c` ceiling 1722 → 1580 (current effective 1580);
  - `rt_async_task.c` removed (282 effective — normal gate);
  - `rt_net.c` removed (666 effective — inside the acceptable band).
- `rt_async_waiter.c` sits at 573 effective — inside the normal gate; the
  Task 11 worry about it breaching 500 raw did not survive the B2 split.
- `scripts/ldflags.sh`: the `-ldflags` value is parsed by Go's
  quoted.Split, which does not understand shell `'"'"'` concatenation, so
  quotes are now stripped from the embedded commit message instead of
  escaped (the B4 commit subject contained an apostrophe and broke
  `make build`).
- Sentrux reconciliation (all rules pass): repo 6182 → 6174, `runtime`
  5340 → 5296, `runtime/native` 5467 → 5389. Drift is the new-file split
  churn; no rule regressions.

## Checks

`./check_file_sizes.sh -a` 100% good; sentrux scans pass; `make build`
green with the sanitized ldflags; `make check` pre-commit.

## Success Criteria

Gate green with tightened ceilings; sentrux recorded; own commit.
