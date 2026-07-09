# Epic 13 Task 3: Transport Contract Tests And Park/Wake Proof

**Status:** complete as of 2026-07-09.
**Kind:** runtime proof-test scaffolding (test-first for Task 4).
Static/pending proof-shape rows now; deterministic behavioral positives and
negative controls become executable in Task 4.
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

## Task 3 Result

Task 3 adds the C-only transport contract seam and the static/pending
proof-shape fixtures without implementing the inbound queue/spine. The runtime
seam lives in `runtime/native/rt_transport.h` and
`runtime/native/rt_transport.c`; the stub returns
`RT_TRANSPORT_STATUS_PENDING_SPINE` or
`RT_TRANSPORT_STATUS_UNAVAILABLE`, reports zero inbound work, and exposes
transport wake counters separately from net wake counters.

The Task 4 sync-point windows are allowlisted now, but their current call sites
are stubs in `rt_transport.c` only:

- `SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK`
- `SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK`
- `SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD`
- `SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE`
- `SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND`
- `SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE`

Passing Task 3 static/pending-shape gate:

```sh
make runtime-v2-transport-contract-check
```

Expected-failing Task 4 acceptance command, intentionally outside normal CI:

```sh
go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$'
```

Current expected diagnostic until Task 4 replaces the stub seam:

```text
pending-spine: <row> requires Task 4 inbound transport spine (rt_transport_enqueue returned RT_TRANSPORT_STATUS_PENDING_SPINE)
```

The opt-in acceptance rows enumerate the Task 4 behavioral contract matrix:
lost-wake seq-cst proof plus skip-recheck/relaxed-ordering negatives, wake
elision running/PARKED positives plus wake-written/skipped negatives,
PARKED-with-inbound-work positive/negative, shutdown wake positive/negative,
and reply-wait task-suspend positive/negative. In Task 3 they are pending
sentinels only; a `pending-spine` result is expected and not a behavior pass.

## Evidence

Verification from this implementation pass:

- `gofmt` on the new Go tests: passed.
- `clang-format` on touched native C/header files: passed.
- `git diff --check`: passed.
- `./check_sync_points.sh`: passed; allowlist matches 13 header enumerators,
  all call sites are in their windows, and the release build has no
  `rt_sync_point_reach` symbol.
- `make runtime-v2-syncpoint-check`: passed.
- `make runtime-v2-transport-contract-check`: passed; 4/4
  `runtime_v2_pending` transport static/pending-shape rows green.
- `make runtime-v2-crossing-check`: passed explicitly; Task 4+ closeouts that
  touch lowering should continue to run it explicitly.
- Expected-red acceptance command:
  `go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$' -count=1 -v --timeout 60s`
  failed as intended with `pending-spine: ... requires Task 4 inbound transport
  spine (rt_transport_enqueue returned RT_TRANSPORT_STATUS_PENDING_SPINE)` and
  C stderr `pending-spine: rt_transport_enqueue has no inbound spine yet`.
- `make c-check`: passed.
- `make cppcheck`: failed on pre-existing style findings outside Task 3
  (`runtime/native/rt_net.c:125`, `runtime/native/rt_net_handles.c:195`,
  `runtime/native/rt_net_handles.c:352`). `rt_transport.c` had no cppcheck
  finding.
- `make check`: passed.
- `./check_file_sizes.sh -a`: passed; 747 files checked, 714 under the good
  threshold, 28 acceptable, 5 legacy ceilings, 0 over limit.
- Sentrux: root `sentrux check .` passed with quality `6189`; scoped runtime
  `sentrux check runtime` passed with quality `5343`; scoped native
  `sentrux check runtime/native` passed with quality `5458`.

## Stop Conditions

- A deterministic proof requires a second real transport consumer (data
  lane) — stop; credits/data-lane proofs belong to the remote-channel epic
  (the epic's bounded-transport decision).
- Sync points cannot express an ordering window without production-code
  changes beyond instrumentation — coordinate with Task 4 instead of
  weakening the proof to sleep/timing.
