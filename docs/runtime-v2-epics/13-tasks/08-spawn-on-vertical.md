# Epic 13 Task 8: `spawn on` Executable Vertical

**Status:** pending.
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

## Proof

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
