# Task 13: Runtime V2 Accept CI Gates

**Status:** Draft
**Kind:** CI
**Depends on:** Task 4, Task 5, Task 12

## Context

The existing `Makefile` pattern for Runtime V2 stable gates (lines ~86-115)
is a top-level `runtime-v2-check` target that runs a fixed MT-test regex,
then calls three sub-targets in sequence:

```makefile
runtime-v2-check:
	...
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm \
	  -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$$' \
	  -count=1 -parallel=1 -p=1 -v --timeout 120s
	$(MAKE) runtime-v2-heap-check
	$(MAKE) runtime-v2-waiter-check
	$(MAKE) runtime-v2-fd-registry-check

runtime-v2-fd-registry-check:
	...
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm \
	  -run '^TestRuntimeV2FDRegistry(...)$$' \
	  -count=1 -parallel=1 -p=1 -v --timeout 180s
```

This task adds the fourth sibling, `runtime-v2-accept-check`, following the
exact same shape: `SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`,
`-tags runtime_v2_pending` for tests that still carry the pending tag (some
of Task 4's tests should have lost the tag by now since Tasks 6-11
implemented them; only genuinely still-future behavior, if any remains,
stays tagged), `-count=1 -parallel=1 -p=1`, an explicit `--timeout`, wired
into the top-level `runtime-v2-check` target so CI picks it up automatically
(CI already runs `make runtime-v2-check` per `LIVENESS_PROBES.md`'s "FD
registry CI liveness gate" row).

## Goal

Add a stable, deterministic `runtime-v2-accept-check` Make target covering
the smallest reliable multi-shard accept subset, and wire it into
`runtime-v2-check` so CI runs it automatically.

## Why This Task Exists

Every prior epic closed with its own CI sub-target (`runtime-v2-heap-check`,
`runtime-v2-waiter-check`, `runtime-v2-fd-registry-check`) so that its
stable contracts stay proven on every future change, not just at the moment
the epic closed. Without this task, Epic 6's hard-won multi-shard accept
guarantees could silently regress in a later epic with nobody noticing until
a benchmark or production incident surfaces it.

## Scope

- Select the smallest deterministic subset of Task 4's contract tests
  (now mostly un-tagged after Tasks 6-11 implemented them) plus Task 5's
  static gates that together prove the Accept Ownership Contract's
  mechanically-checkable properties: `SURGE_SHARDS=1` compatibility,
  `SURGE_SHARDS=N` initialization and rejection behavior, owner-shard fd
  registry placement, no-steal for connection tasks, and shutdown drain
  across shards.
- Explicitly exclude from this required gate anything the epic's own Proof
  And Quality Contract or Accepted Baseline Debt sections mark as
  local-only (e.g. timing-sensitive `SIGUSR1` live probes, heavy real-load
  benchmark rows, anything still gated `runtime_v2_pending` because a
  follow-up task or later epic owns it).
- Add `runtime-v2-accept-check` to the `Makefile` following the exact
  `SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, `-count=1
  -parallel=1 -p=1`, explicit `--timeout` shape of its siblings.
- Wire it into the top-level `runtime-v2-check` target, in the same
  `$(MAKE) runtime-v2-<x>-check` chain as the existing three.
- Update `LIVENESS_PROBES.md`'s "Existing Usable Probes" table with a new
  row for this gate, following the "FD registry CI liveness gate" row's
  format exactly (Probe / Purpose / Current evidence-source / Required
  command / Pass condition / Blocker status / Future owner).
- Confirm the accepted broad-VM-command debt (`RV2-DEBT-001`) is not
  silently required by this new gate — this task must not add
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required check.

## Out Of Scope

- Any new test or trace counter — Tasks 4, 5, 12 already produced everything
  this task selects from. This task only promotes a subset into a stable,
  named, wired-in gate.
- Runtime code changes.

## Approach / Steps

1. Enumerate every Task 4/Task 5 test that is currently untagged (i.e.
   Tasks 6-11 made it real) and stable across at least 3 consecutive local
   runs (watch for `RV2-DEBT-011` artifact-collision flakiness before
   trusting a single green run).
2. Select the smallest subset that still covers every mechanically-checkable
   Accept Ownership Contract bullet; do not include a test whose only
   purpose is redundant with another already-selected test.
3. Add the `runtime-v2-accept-check` Make target with the exact regex/tag/
   flag shape of its siblings.
4. Wire it into `runtime-v2-check`.
5. Run `make runtime-v2-check` end to end at least 3 times to confirm the
   new sub-target does not introduce flakiness into the whole chain.
6. Update `LIVENESS_PROBES.md` with the new probe row.
7. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `Makefile`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`

Read:

- `internal/vm/runtime_v2_accept_contract_test.go` (Task 4, as landed after
  Tasks 6-11)
- `internal/vm/runtime_v2_skeleton_static_test.go`, the new net-ownership
  static gate file (Task 5, as landed)
- `docs/runtime-v2-epics/DEBT.md` (`RV2-DEBT-001`, `011`)

## Skills & Working Practice

- This is a low-risk, mechanical task (selecting and wiring existing tests),
  so the plan-gate can be lighter than for runtime-code tasks, but the
  subset selection itself (which tests are "stable enough" and "non-
  redundant") is a judgment call worth stating explicitly before finalizing
  the Makefile change.
- Run the full chain multiple times before declaring it stable — a single
  green run is not evidence of determinism, especially given `RV2-DEBT-011`'s
  known artifact-collision risk under concurrent same-named test runs.

## Checks

- `make runtime-v2-accept-check` (new target, run standalone)
- `make runtime-v2-check` (full chain, run at least 3 times)
- `make check`
- `git diff --check`

## Definition Of Done

- [ ] `runtime-v2-accept-check` exists in the `Makefile`, matching its
      siblings' exact flag shape.
- [ ] It is wired into `runtime-v2-check` and runs automatically under CI.
- [ ] The selected subset is stable across at least 3 consecutive local
      runs.
- [ ] `LIVENESS_PROBES.md` has a new row for this gate in the same format as
      the existing "FD registry CI liveness gate" row.
- [ ] The accepted broad-VM-command debt is not silently required by the new
      gate.

## Evidence To Record

- `06-evidence.md`: the exact test subset selected and why, Commands/Checks
  (3+ stable runs), Follow-Ups And Blockers (anything explicitly excluded
  and why).
- `NOTES.md`: the new gate's name and command, for quick recall in future
  epics.
