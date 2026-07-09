# Epic 13 Task 3: Transport Contract Tests And Park/Wake Proof

**Status:** pending.
**Kind:** runtime proof tests (test-first for Task 4). Deterministic
positive proofs + negative controls; sync-point pattern.
**Depends on:** Task 1 (invariants), Task 2 (trustworthy harness).

## Goal

Before the transport spine exists, write the deterministic tests and negative
controls that define its correctness: seq-cst enqueue/PARKED ordering, wake
elision without lost wakes, PARKED-with-inbound-work detection, shutdown wake,
and the task-suspend-vs-shard-park distinction. Task 4 is done only when these
tests pass; the negative controls prove the tests can actually catch the bug.

## Contract Under Test (from RUNTIME_V2 §8 and the epic)

Consumer park: drain inbound; store `state = PARKED` (seq-cst); re-check
inbound after the store; non-empty -> store `RUNNING`, loop without sleeping;
empty -> `poll()` with the wake fd; on wake store `RUNNING`, drain wake fd.

Producer send: publish the complete message (seq-cst with the PARKED store);
load park state; `PARKED` -> write wake fd; `RUNNING` -> elide.

Debug invariant: no PARKED shard may have a non-empty inbound queue at a
safepoint.

Reply-wait invariant: a caller waiting for a transport reply is a suspended
TASK (waiter keyed on the reply), never a shard park; on a self-crossing
destination the shard keeps draining its own inbound queue.

## Starting State (verify and re-pin)

- Sync-point mechanism: `runtime/native/rt_sync_point.h` (`RT_SYNC_POINT_SP_*`
  enum) + `rt_sync_point.c`; static gate `check_sync_points.sh`
  (`runtime-v2-syncpoint-check`). Epic 9 precedent for
  positive-proof + negative-control pairs: `RV2-DEBT-023`
  (`TestRuntimeV2LifecycleDebt023CancelParkWakeTokenProof` /
  `...NegativeControl`), `RV2-DEBT-020`, `RV2-DEBT-022`.
- Shard park/wake today: `rt_task_park.c`, `rt_shard.wake_pending`
  (`rt_async_internal.h:92`), `net_poll_wake` (`:127`, `:162`), atomic
  `wake_token` (`:196`). The transport park state is NEW state (Task 4);
  tests here define its observable behavior.
- Negative-control precedent: compile-time flag builds the known-broken
  ordering (e.g. `RV2_DEBT_023_NEGATIVE_CONTROL`) and the test asserts the
  strand is detected.

## Scope

In: new test files under `internal/vm` (runtime rows) and/or native test
hooks; new `SP_*` sync points ONLY as test-only instrumentation stubs whose
production sites land in Task 4; `check_sync_points.sh` window map updates.

Out: the transport implementation itself (Task 4); any lowering; any
crossing syntax in test programs (runtime-level API tests only — supported
crossing forms cannot execute yet).

## Steps

### 1. Proving spike plan

Record hypothesis, files, proof command, success/failure criteria, rollback
note. The tests will initially target a minimal internal test-only transport
surface (Task 4 defines the real one); agree the function-level seam with
Task 4's author first so tests do not need rewriting.

### 2. Lost-wake pair

- Positive: producer enqueues while consumer is between "drain" and "PARKED
  store" (sync point), and the consumer still observes the message (re-check
  path) or the wake fires — no strand, bounded time.
- Negative control: a build flag downgrades the enqueue/PARKED ordering to
  relaxed/acquire-release or skips the re-check; the test must then detect
  the strand deterministically via sync points (not by timeout luck).

### 3. Wake elision pair

- Positive: N messages to a RUNNING consumer produce zero wake-fd writes
  (counter), and the messages are still drained.
- Positive: message to a PARKED consumer produces exactly one wake-fd write
  and a bounded wake.
- Counters asserted via the transport trace counters Task 4 adds (agree
  names now: enqueue, wake writes, wake elisions).

### 4. PARKED-with-inbound-work invariant

Debug-build assertion (or safepoint check) that fires when a shard is PARKED
with a non-empty inbound queue; a test that would trip it under the broken
ordering (reuse the Step 2 negative control), and proof it stays silent under
the correct one.

### 5. Shutdown wake

Runtime shutdown wakes a transport-parked shard and a transport-reply-waiting
task on every shard; no waiter sleeps through shutdown. Rows for
`SURGE_SHARDS=1,2,8`.

### 6. Reply-wait is a task suspend

A test seam where a task on shard S waits for a reply that only shard S can
produce (self-crossing shape at runtime level): the shard must drain its own
inbound queue and complete the reply. This is the N=1 deadlock forcing
function, runnable before lowering exists.

## Proof

- All positive rows green at `SURGE_SHARDS=1,2,8` (after Task 4 lands; until
  then the rows are committed as failing-by-construction behind the Task 4
  seam or a build tag, per the test-first rule — record which).
- Each negative control demonstrably detects its bug (run log in evidence).
- `check_sync_points.sh` green with the new windows.
- `make c-check`, `make cppcheck` if native test hooks are C; `make check`.

## Stop Conditions

- A deterministic proof requires a second real transport consumer (data
  lane) — stop; credits/data-lane proofs belong to the remote-channel epic
  (the epic's bounded-transport decision).
- Sync points cannot express an ordering window without production-code
  changes beyond instrumentation — coordinate with Task 4 instead of
  weakening the proof to sleep/timing.
