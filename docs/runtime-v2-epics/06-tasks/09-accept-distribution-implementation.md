# Task 9: Accept Distribution Implementation

**Status:** Complete
**Kind:** runtime code
**Depends on:** Task 3, Task 6, Task 7, Task 8

## Context

This task turns Task 3's proven spike into real, rule-compliant
implementation. Per Global Rule 1: *"A proving spike must not be silently
promoted into architecture. After the proof run, the team must either accept
the result as a design input, rewrite it into a rule-compliant
implementation, or delete it."* If Task 3 kept any spike code as a seed, this
task is where it gets rewritten to Global Rule 8's owner-first,
explicit-status standard — it must not simply be merged as-is.

By this point in the epic:

- Task 6 has given you `N` real shards, each with a real `fd_registry` and
  `waiter_store` (`rt_async_internal.h:150-160`).
- Task 7 has given you owner-shard task placement and a no-steal boundary.
- Task 8 has given you `NetListener`/`NetConn` structs that carry owner-shard
  metadata and (if chosen) a listener-group discriminator.

What is still missing is the actual mechanism that, at accept time, (a)
creates the fd on the right shard (or accepts it from the right group
member), (b) registers it in that shard's `rt_fd_registry`
(`rt_fd_registry_init`/`rt_fd_registry_attach_net_interest`,
`runtime/native/rt_fd_registry.c`) instead of always shard 0's via
`rt_executor_fd_registry` (`rt_runtime.c:152-156`, currently routes through
`rt_runtime_shard0`), and (c) places the handler task on that same shard
using Task 7's placement mechanism.

`rt_net_listen` (`rt_net.c:413`) currently sets only `SO_REUSEADDR`
(`rt_net.c:435`); if Task 3 chose the `SO_REUSEPORT` group model, this is the
function (or its group-aware successor) that gains `SO_REUSEPORT`.

## Goal

Implement the selected listener model for real and prove accepted fds enter
the owning shard's registry, not a process-wide or shard-0 registry.

## Why This Task Exists

This is the task the Accept Ownership Contract's central sentence describes:
*"Each accepted connection has one owner shard at creation time. The
accepted connection fd is registered in the owning shard's fd registry, not a
process-wide or shard-0 registry."* Everything before this task (6, 7, 8) is
plumbing; this task is where accept ownership actually becomes true at
runtime instead of only true in struct shape.

## Scope

- Implement `SO_REUSEPORT` (plus `SO_REUSEADDR`) listener-group creation on
  Linux if Task 3 chose that model, one socket per shard, all bound to the
  same port, each tagged with its owning shard per Task 8's metadata; or
  implement the documented explicit-handoff fallback if that is what Task 3
  proved out — and if a fallback is used, make the trace/benchmark evidence
  (Task 12) visibly mark it as a fallback, per the Accept Ownership
  Contract: *"Any accept handoff fallback is visible in trace counters and
  benchmark evidence. It must not be described as the ideal `SO_REUSEPORT`
  hot path."*
- Implement whatever internal accept-task/callback mechanism Task 3's spike
  proved (a runtime-internal accept loop per shard, or direct completion
  into a spawn call from the shard's own poller) for real, using owner-first
  APIs and explicit status codes (Global Rule 8) instead of the spike's
  throwaway shape.
- On accept, register the new connection's fd into the accepting shard's
  `rt_fd_registry` using a shard-indexed accessor (from Task 6) rather than
  the shard-0 compatibility path.
- Place the handler task on the accepting shard using Task 7's placement
  mechanism, with zero required changes to the user's own Surge source code
  (the accept-loop-in-user-code / no-new-syntax question Task 3 already
  answered — this task must actually deliver that answer, not merely repeat
  it).
- Confirm a non-owner task cannot silently operate on a shard-owned
  connection through shard 0 or an implicit handoff; either add the guard
  the Accept Ownership Contract requires, or record it as explicit debt
  before closeout (the contract allows either, but not silence): *"A
  non-owner task must not silently operate on a shard-owned connection
  through shard 0, a global lock fallback, or an implicit handoff. The task
  must either be rejected by an Epic 6 guard or recorded as explicit future
  debt before closeout."*
- Flip the remaining Task 4 pending contract tests that depend on real
  accept distribution (distribution-across-shards, owner-registry
  placement, non-owner-rejection) to passing, or update them if empirical
  behavior differs from what Task 4 guessed before this task existed (record
  any such correction explicitly, do not silently rewrite a test to match
  whatever the implementation happened to do).

## Out Of Scope

- Per-shard poller/wake mechanism itself (Task 10) — this task can assume
  Task 10 will exist to drive readiness, or land net polling changes
  together with Task 10 if the dependency turns out to be tighter than
  expected in practice (if so, merge the two tasks explicitly and record
  why, rather than silently blurring their scopes).
- Close/cancellation/shutdown migration to the per-shard model in full
  (Task 11) — this task only needs enough close-path correctness that its
  own tests do not leak fds/processes; the durable close/cancel/shutdown
  contract is Task 11's job.
- Trace counters and benchmark rows (Task 12).

## Approach / Steps

1. Confirm Tasks 3, 6, 7, 8 are all landed and their decisions are
   compatible (re-read each task's `06-evidence.md` entry for drift).
2. Implement (or finalize, if a spike seed is being rewritten) listener
   creation for the chosen model.
3. Implement fd-to-owner-shard registration at accept time, using the
   shard-indexed `rt_fd_registry` accessor.
4. Implement handler-task placement on the accepting shard.
5. Add the non-owner-access guard, or write the explicit debt entry if it is
   deferred.
6. Run a real multi-shard accept burst (e.g. 8-32 simultaneous connections
   against an `N=4` or `N=8` shard runtime) and confirm empirically which
   shard's registry each connection lands in; this is the first point in the
   epic where "does accept ownership actually work" can be observed, not
   just structurally implied.
7. Flip/update Task 4's pending tests.
8. Update `06-evidence.md` and `NOTES.md`, including any correction to Task
   3's or Task 4's assumptions found during real implementation.

## Files

Touch:

- `runtime/native/rt_net.c` (listener creation, accept path, fd registration
  call sites)
- Any new small C file this task introduces for the internal accept-task
  mechanism, kept at or below the Global Rule 4 500-line limit and not a
  vague `helpers`/`common` catch-all (Global Rule 4)

Read:

- `docs/runtime-v2-epics/06-listener-model-proving-spike.md` (Task 3)
- `docs/runtime-v2-epics/06-evidence.md` (Tasks 6, 7, 8 entries)
- `runtime/native/rt_fd_registry.h`, `rt_fd_registry.c`
- `internal/vm/runtime_v2_accept_contract_test.go` (Task 4)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state exactly which spike code (if any) is
  being rewritten vs. written fresh, and the exact accept-task mechanism,
  before implementation starts.
- This is the highest-risk implementation task in the epic — it is where
  three prior tasks' decisions (listener model, scheduler placement, owner
  metadata) must compose correctly for the first time. Budget for the real
  multi-shard accept burst test to reveal a mismatch between what Tasks 3/6/
  7/8 assumed and what actually happens; if it does, fix the mismatch here
  and record the correction rather than treating an earlier task's
  assumption as sacred.
- Sequenced after Tasks 3, 6, 7, 8; may proceed in parallel with the early
  part of Task 10 only if the poller/wake split turns out to be clean in
  practice — default to sequencing them unless there is a concrete reason
  not to.
- `RV2-DEBT-011` (VM/LLVM build/test artifact collisions under concurrent
  same-name test runs) is relevant if this task's new tests share a name
  pattern with existing ones — check before assuming a flaky new test result
  is a real regression.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `go test ./internal/vm -run 'TestRuntimeV2Accept'` (all distribution/
  registry-placement cases should now pass)
- Real multi-shard accept burst probe (new, ad hoc or promoted into Task 4's
  suite)
- `git diff --check`
- Sentrux root and scoped scans

## Definition Of Done

- [ ] The chosen listener model is implemented for real (not spike code) and
      uses owner-first APIs with explicit status codes.
- [ ] Accepted connections empirically land in their accepting shard's
      `fd_registry`, proven by direct observation across at least 2 shards
      under a real connection burst.
- [ ] Handler tasks run on the connection's owner shard with zero required
      changes to user Surge source code.
- [ ] Any accept-handoff fallback (if used instead of `SO_REUSEPORT`) is
      visibly marked as a fallback in trace/benchmark evidence, not
      presented as the ideal hot path.
- [ ] Non-owner access to a shard-owned connection is either rejected by a
      guard or recorded as explicit debt before closeout — not silent.
- [ ] Remaining relevant Task 4 pending tests pass, with any correction to
      earlier tasks' assumptions recorded explicitly.

## Evidence To Record

- `06-evidence.md`: Contracts Touched (accept distribution, owner-registry
  placement, non-owner guard/debt), Commands/Checks, the real multi-shard
  burst observation with exact per-shard connection counts.
- `NOTES.md`: any correction to Tasks 3/6/7/8's assumptions found here, and
  the final non-owner-access decision (guard vs. debt).
