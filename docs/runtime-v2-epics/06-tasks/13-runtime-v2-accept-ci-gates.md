# Task 13: Runtime V2 Accept CI Gates

**Status:** Complete
**Kind:** CI
**Depends on:** Task 4, Task 5, Task 12

## Context

At Task 13 start, `runtime-v2-accept-check` already existed and was already
called by `runtime-v2-check`. Earlier Epic 6 tasks had added the target while
promoting metadata, static, and net-poller checks. Task 13 therefore closed as
an audit/refinement task, not a from-scratch target creation task.

The stable Runtime V2 gate shape remains:

```makefile
runtime-v2-check:
	...
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm \
	  -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$$' \
	  -count=1 -parallel=1 -p=1 -v --timeout 120s
	$(MAKE) runtime-v2-heap-check
	$(MAKE) runtime-v2-waiter-check
	$(MAKE) runtime-v2-fd-registry-check
	$(MAKE) runtime-v2-accept-check

runtime-v2-accept-check:
	...
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm \
	  -run '^TestRuntimeV2...$$' \
	  -count=1 -parallel=1 -p=1 -v --timeout <bounded>
```

This task refined the target so it also covers the Task 12 stable accept trace
contracts and Task 7/12 scheduler placement/no-steal contracts.

## Goal

Keep `runtime-v2-accept-check` as a stable, deterministic sibling gate that
covers the smallest reliable multi-shard accept subset and still runs
automatically through `runtime-v2-check`.

## Why This Task Exists

Every prior epic closed with its own CI sub-target (`runtime-v2-heap-check`,
`runtime-v2-waiter-check`, `runtime-v2-fd-registry-check`) so that its
stable contracts stay proven on every future change, not just at the moment
the epic closed. Without this task, Epic 6's hard-won multi-shard accept
guarantees could silently regress in a later epic with nobody noticing until
a benchmark or production incident surfaces it.

## Scope

- Select the smallest deterministic subset of Task 4/5/12 tests that together
  prove the Accept Ownership Contract's mechanically-checkable properties:
  `SURGE_SHARDS=1` compatibility, `SURGE_SHARDS=N` initialization and rejection
  behavior, owner-shard fd registry placement, no-steal for connection tasks,
  no shard-0 fallback, per-shard net-poller wake, and shutdown drain across
  shards.
- Explicitly exclude from this required gate anything the epic's own Proof
  And Quality Contract or Accepted Baseline Debt sections mark as
  local-only (e.g. timing-sensitive `SIGUSR1` live probes, heavy real-load
  benchmark rows, broad VM regex gates, and later-epic owner-generation
  guards).
- Keep `runtime-v2-accept-check` in the `Makefile` with the exact
  `SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, `-count=1 -parallel=1
  -p=1`, explicit `--timeout` shape of its siblings.
- Keep it wired into the top-level `runtime-v2-check` target, in the same
  `$(MAKE) runtime-v2-<x>-check` chain as the existing siblings.
- Update `LIVENESS_PROBES.md`'s "Existing Usable Probes" table with a new
  row for this gate, following the "FD registry CI liveness gate" row's
  format exactly (Probe / Purpose / Current evidence-source / Required
  command / Pass condition / Blocker status / Future owner).
- Confirm the accepted broad-VM-command debt (`RV2-DEBT-001`) is not
  silently required by this new gate — this task must not add
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required check.

## Out Of Scope

- Any new test or trace counter. Tasks 4, 5, 12 already produced everything
  this task selects from. This task only promotes a subset into a stable,
  named, wired-in gate.
- Runtime code changes.
- Closing `RV2-DEBT-013`. The raw/copied net-handle owner guard remains open
  and belongs to a later net-handle or stdlib owner-local design task.

## Approach / Steps

1. Confirm the existing Makefile target and top-level wiring.
2. Add the missing stable Task 12 accept trace tests and scheduler
   placement/no-steal tests to the target.
3. Split the target into sequential focused `go test` commands so regexes stay
   readable and `RV2-DEBT-011` artifact collisions are not amplified by
   overlapping VM/LLVM runs.
4. Update `LIVENESS_PROBES.md` with the new probe row and Task 12 trace fields.
5. Update `06-evidence.md`, `NOTES.md`, and the task index.
6. Run the standalone gate and the three full `runtime-v2-check` stability
   passes before commit.

## Files

Touch:

- `Makefile`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/06-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/06-tasks/README.md`
- `docs/runtime-v2-epics/06-tasks/13-runtime-v2-accept-ci-gates.md`

Read:

- `internal/vm/runtime_v2_accept_contract_test.go` (Task 4, as landed after
  Tasks 6-11)
- `internal/vm/runtime_v2_accept_compat_test.go`
- `internal/vm/runtime_v2_accept_static_test.go`
- `internal/vm/runtime_v2_net_metadata_test.go`
- `internal/vm/runtime_v2_net_poller_static_test.go`
- `internal/vm/runtime_v2_scheduler_placement_test.go`
- `internal/vm/runtime_v2_scheduler_placement_source_test.go`
- `docs/runtime-v2-epics/DEBT.md` (`RV2-DEBT-001`, `011`, `013`)

## Skills & Working Practice

- This is a low-risk, mechanical task (selecting and wiring existing tests),
  so the plan-gate can be lighter than for runtime-code tasks, but the
  subset selection itself (which tests are "stable enough" and "non-
  redundant") is a judgment call worth stating explicitly before finalizing
  the Makefile change.
- Run focused VM/LLVM commands sequentially. `RV2-DEBT-011` makes overlapping
  same-name VM build tests unreliable.
- The implementation subagent ran the standalone target. The main session ran
  full-chain stability passes before commit.

## Checks

- `timeout 300s make runtime-v2-accept-check`
- `timeout 600s make runtime-v2-check` three consecutive times.
- `timeout 600s make check`
- `git diff --check`

## Definition Of Done

- [x] `runtime-v2-accept-check` exists in the `Makefile`, matching its
      siblings' exact flag shape.
- [x] It is wired into `runtime-v2-check` and runs automatically under CI.
- [x] The selected subset is stable across at least 3 consecutive full-chain
      local runs.
- [x] `LIVENESS_PROBES.md` has a new row for this gate in the same format as
      the existing "FD registry CI liveness gate" row.
- [x] The accepted broad-VM-command debt is not silently required by the new
      gate.

## Evidence To Record

- `06-evidence.md`: the exact test subset selected and why, Commands/Checks
  (standalone pass plus 3+ full-chain stable runs), Follow-Ups And Blockers
  (anything explicitly excluded and why).
- `NOTES.md`: the new gate's name and command, for quick recall in future
  epics.
