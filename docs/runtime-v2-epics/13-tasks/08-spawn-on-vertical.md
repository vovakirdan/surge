# Epic 13 Task 8: `spawn on` Executable Vertical

**Status:** complete.
**Kind:** LLVM lowering + runtime e2e. Gates together with Task 9 — the
crossing guard for `spawn on` does NOT flip publicly in this task.
**Depends on:** Task 6 (publication API), Task 7 (representation).

## Goal

Lower `spawn on shard(id) { ret value; }` and `spawn on distributed { ret
value; }` on `BackendLLVM` to the Task 6 publication API and prove, end to
end from Surge source: remote owner placement, publication wait as a task
suspend, and a live affine `far Task<T>` handle with its generation token.
Executable only under a test-scoped capability override until Task 9
delivers await/cancel — no publicly minted handles without a discharge path.

## Starting State (verify and re-pin)

- Task 6 APIs and statuses; Task 7 HIR/MIR nodes and per-form capability
  predicate; Task 5 placement encoding.
- Sema capture verdicts and payload types in
  `CrossingLoweringInfo.Captures/PayloadType` — codegen consumes these; the
  Task 1 payload decision (plain-data/copyable unless proven) binds here.
- `ret` semantics from Epic 11: block-local result; `return` cannot escape.

## Scope

In: LLVM codegen for the `spawn on` HIR/MIR path (capture materialization,
placement resolve call, publish call, suspend on publication ack, handle
value construction), runtime glue, e2e tests under the test-scoped
capability override, failure-status mapping (shutdown/refused/queue-full ->
deterministic runtime error, never local spawn).

Out: flipping `backendSupportsCrossingForm(LLVM, spawn_on)` for production
compiles (Task 9's joint gate), await/cancel (Task 9), immediate `on`
(Task 10), `pool` execution, VM support, heap-owned payload moves unless
Task 1 recorded the safety proof.

## Steps

1. **Test-first** e2e rows (Surge source, compiled with the override,
   `SURGE_SHARDS=1,2,8` via the Task 2-hardened harness):
   - `spawn on shard(k)` runs the body on shard k (owner id observable via
     trace/test hook), returns a handle;
   - `spawn on distributed` at shards>1: at least one run proves a
     non-caller destination (policy proof);
   - self-crossing (`shard(current)` and shards=1): publication completes
     without deadlock;
   - capture matrix: copyable capture, plain-data owned capture; (heap-owned
     capture only if Task 1 proved it — otherwise a compile-level row proves
     it is rejected/represented per the payload decision);
   - failure rows: publish during shutdown -> deterministic error; the
     out-of-range `shard(id)` rule behaves exactly as Task 1 decided;
   - no-hidden-fallback negative row: with the capability OFF, the same
     source still produces FUT7015 and does not execute.
2. Implement codegen: materialize captures per representation, resolve
   placement, call publish, suspend on the pending ack (transport resume
   kind), construct the `far Task<T>` value (handle + token).
3. Map failure statuses to deterministic runtime errors with stable text.
4. Trace evidence: publication counters increment; owner histogram shows the
   destination shard.

## Closeout

Implemented the executable `spawn on` lowering spine behind the
test-scoped crossing override. The production capability guard remains closed:
normal LLVM/VM compiles still report FUT7015 for `spawn on` until Task 9 lands
the `far Task.await()` / `far Task.cancel()` discharge path and flips the joint
gate deliberately.

What landed:

- `CompileRequest.CrossingFormsForTest` and corresponding HIR/MIR combine,
  lower, and validate options, including dependency-module coverage.
- MIR `InstrCrossing` state/pending fields plus a synthetic remote poll
  function/state record for `spawn on`.
- LLVM lowering to `rt_remote_spawn_publish_placement`, including retry
  through the persistent pending slot, async task suspension, handle allocation,
  status-to-panic mapping, and no local spawn fallback.
- Runtime placement publication wrapper with distinct unsupported/invalid
  placement statuses.
- ABI/static proof that `rt_far_task_handle` is exactly the shape allocated by
  codegen.

Boundaries kept explicit:

- Full source-level user e2e that consumes the returned `far Task<T>` waits for
  Task 9, because Task 8 intentionally does not open await/cancel.
- Non-async/synchronous `fn -> far Task<T>` bodies remain guarded in production
  and are not a supported executable vertical in this task.
- `pool`, immediate `on`, far-handle `on`, remote channels/select, distributed
  scope accounting, and remote-free ownership remain out of scope.

Verified:

- `go test ./internal/mir -run 'Crossing|SpawnOn' -count=1`
- `go test ./internal/buildpipeline -run 'Crossing|SpawnOn' -count=1`
- `go test ./internal/backend/llvm -run 'Crossing|SpawnOn|Placement' -count=1`
- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2RemotePublication(APIShape|Behavior|FailurePathStaticGuards)$' -count=1`
- `go test ./internal/driver -run 'Remap|HIR|Crossing|Module' -count=1`
- `make runtime-v2-crossing-check`
- `make c-check`
- `make cppcheck`
- `make check`
- `sentrux check .` quality `6184`
- `sentrux check internal` quality `6528`
- `sentrux check runtime` quality `5360`
- `sentrux check runtime/native` quality `5484`

## Joint Gate Proof Carried To Task 9

The original executable rows below require both publication and the
`far Task<T>` discharge path. Task 8 landed the publication/codegen side and
kept the public guard closed; Task 9 owns completing these rows and flipping
the joint production capability.

- All Step 1 rows green at `SURGE_SHARDS=1,2,8`.
- Publication wait proven a task suspend: the self-crossing row plus a row
  where the caller shard processes other work while a publication to a busy
  destination is pending.
- `make runtime-v2-crossing-check` still green in production state (guard
  not flipped).
- `make c-check`, `make cppcheck`, `make golden-check` if fixtures,
  `./check_file_sizes.sh -a`, Sentrux scoped scans, `make check`.

## Stop Conditions

- The vertical needs scope enrollment or completion messages to the caller's
  scope — stop: severability contract violation; design review.
- Payload materialization needs remote-free — stop: payload decision
  violated; return to Task 1's record.
