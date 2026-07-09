# Epic 13 Task 4: Inbound Transport Spine

**Status:** complete in this worktree.
**Kind:** native runtime. The spine only — no crossing lowering, no
publication protocol yet.
**Depends on:** Task 3 (tests exist and define done).

## Goal

Add the per-shard inbound transport: bounded data queue with a reserved
control lane, an OS-neutral wake abstraction/counter surface, the seq-cst park
protocol, worker-loop drain points, transport debug counters, and shutdown
wake cleanup — passing the minimum complete Task 4 rows for the shard-locked
first spine.

## Starting State (verify and re-pin)

- `struct rt_shard` (`runtime/native/rt_async_internal.h:149`) — where the
  inbound queue, transport park state, and wake handle live.
- Existing wake plumbing: `rt_net_poll_wake` (`:127`), `wake_pending`
  (`:92`), `rt_task_park.c` shard sleep. The transport wake must be counted
  separately from net poll wakes (epic invariant).
- Worker loop: `rt_worker_turn.c` — where drain points slot in relative to
  ready-queue pop, net poll, and sleep.
- Lock lanes: `rt_lane.c` — control -> at most one shard lock; the inbound
  queue's synchronization must state its lane position and not violate the
  asserted order.
- Trace counters: `rt_async_trace.c` — follow the existing counter shape.

## Design Constraints (from the epic; restate in code comments where they bind)

- Bounded queue; control-lane messages (completion, cancel, credit-return,
  shutdown wake) can never be blocked behind data-lane backpressure. Credit
  ACCOUNTING is a non-promoted spike: build the bounded queue + control lane,
  do not promote credit machinery nothing exercises.
- Enqueue publishes the complete message before any wake decision; PARKED
  store and enqueue carry seq-cst ordering; producer wake observes PARKED
  before writing the wake fd.
- Wake abstraction is OS-neutral in the public shape. Task 4 uses a
  pipe-backed `rt_transport_wake` as a counter/drain surface, but actual worker
  wake is deliberately delivered through the existing shard `wake_pending` /
  `worker_cv` path because the current worker idle path does not sleep on a
  transport pollset.
- Each message has exactly one owning free path.
- Transport wake writes / elisions / enqueues are new counters, separate from
  net poll counters.
- A shard drains its OWN inbound queue in its worker loop — this is what the
  reply-wait invariant and the N=1 self-crossing row rely on.

## Scope

In: `runtime/native/rt_transport.c/.h` for the queue+park protocol and
pipe-backed wake abstraction, `rt_shard` by-value transport state,
worker-loop drain points, debug counters, shutdown path, sync-point
production sites for Task 3's windows.

Out: message payload semantics beyond an opaque envelope (publication is
Task 6), placement ABI (Task 5), compiler work, credits accounting, remote
channel/data-lane machinery.

## Steps

### 1. Message envelope and queue

Define the envelope (category enum matching the epic's transport table rows
that this epic implements: spawn request/ack, completion, cancel
request/ack, immediate execute request/reply, shutdown/control; leave credit
categories declared but unimplemented), the bounded MPSC (or shard-locked)
queue per the Task 1 spike outcome, and the control-lane reservation
(separate ring / reserved capacity / separate queue — record the choice and
why the control lane cannot be starved).

### 2. Park protocol

Transport park state on `rt_shard` (atomic; states at least RUNNING/PARKED),
integrated with the existing shard sleep so there is ONE place a shard
decides to sleep: drain inbound -> drain ready -> poll prep -> PARKED store
(seq-cst) -> inbound re-check -> sleep or loop. Producer path: enqueue
(publish complete, seq-cst) -> load park state -> wake fd write iff PARKED.
Because Task 4 intentionally uses the target shard lock as the queue
serializer, a producer cannot enter the PARKED->recheck window while the
consumer holds that lock; the executable proof therefore covers the
shard-locked recheck and the condvar wake path, not a future lock-free MPSC
race.

### 3. Wake abstraction

`rt_transport_wake` interface: create/destroy, write, drain. Pipe
implementation first; the fd is not added to the shard net pollset in Task 4.
Worker wake remains `wake_pending` + `worker_cv`, while the pipe stays as the
transport abstraction/counter/drain surface.

### 4. Drain points and shutdown

Worker loop drains inbound before considering idle sleep; shutdown wakes every
shard and records `shutdown_wakes`; destroy tears down the queue/wake state.
The current payload is opaque/shallow, so Task 4 does not invent payload free
semantics.

### 5. Counters and debug invariant

Counters: `enqueue_count`, `control_enqueue_count`, `data_enqueue_count`,
`drain_count`, `transport_wake_writes`, `transport_wake_elisions`,
`shutdown_wakes`, wake drain/failure counters, plus the
PARKED-with-inbound-work debug assertion extended to transport inbound.

### 6. Make Task 3 green

Wire the sync-point production sites; run the minimum complete Task 4 C
acceptance rows, including deterministic negative controls.

## Result (2026-07-09)

Implemented:

- `rt_shard.transport` embeds `rt_transport_state` by value and is
  initialized/destroyed from `rt_runtime.c`.
- `rt_transport.c` owns a shard-locked bounded data queue plus separate
  reserved control queue. Control drains before data and can still enqueue when
  the data lane is full.
- Producer enqueue takes only the target shard lock, publishes the complete
  message, uses the transport sync-point windows, loads park state with the
  seq-cst protocol, wakes only `PARKED` shards, and elides `RUNNING` wakes.
- Consumer paths drain a bounded transport slice before ready-work selection
  and fully drain before idle sleep; the single-runner `run_ready_one` path
  does the same bounded drain before ready-pop.
- Consumer idle path publishes `PARKED`, rechecks inbound, and returns to
  `RUNNING` if work exists. Under the shard-locked first spine, the recheck is
  still present and tested, but producer concurrency in that exact window is
  serialized by the shard lock.
- Shutdown wakes all shards through the transport path and records
  `shutdown_wakes`.
- Reply-wait is represented as a task-suspend seam only; no shard park or
  placement/publication ABI was added.

Deliberate boundary:

- The pipe-backed `rt_transport_wake` is not wired into `rt_net_poll_pass`.
  Current workers sleep on `worker_cv`; correctness wake delivery uses the
  existing shard wake token/`worker_cv` path. The pipe remains available for
  future pollset integration and for wake write/drain counters.
- The acceptance tests are native C transport-spine tests, not placement or
  publication tests. They do not claim that compiler-lowered `spawn on` or
  `on` bodies execute yet.

Debt:

- No new debt recorded. Future placement ABI, publication protocol, pollset
  integration, payload ownership, and credit accounting remain in their
  existing later Epic 13 task scopes rather than Task 4 debt.

Evidence:

- `go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$' -count=1 -v --timeout 120s`
  passed. Rows cover shard-locked recheck shape with sync-point reach, real
  `worker_cv` wake from a transport-PARKED wait, wake elision for RUNNING,
  PARKED wake exactly once, parked-with-inbound invariant, shutdown wake, and
  reply-wait task-suspend seam. Negative controls
  `RT_TRANSPORT_NEG_SKIP_RECHECK`, `RT_TRANSPORT_NEG_RELAXED_PARK_ORDER`,
  `RT_TRANSPORT_NEG_SKIP_PARKED_WAKE`, `RT_TRANSPORT_NEG_WRITE_RUNNING_WAKE`,
  `RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE`, and
  `RT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD` fail deterministically.
- `make runtime-v2-transport-contract-check` passes and is called by
  `make runtime-v2-check`.
- `git diff --check`
- `./check_sync_points.sh`
- `make runtime-v2-syncpoint-check`
- `make c-check`
- `make check`
- `./check_file_sizes.sh -a`
- Sentrux: `sentrux check .` quality `6186`; `sentrux check runtime`
  quality `5353`; `sentrux check runtime/native` quality `5455`; all rules
  pass.

Known unrelated static-analysis result:

- `make cppcheck` still fails only on pre-existing net const-style findings:
  `runtime/native/rt_net.c:125`, `runtime/native/rt_net_handles.c:195`, and
  `runtime/native/rt_net_handles.c:352`. `rt_transport.c` has no cppcheck
  findings after the Task 4 cleanup.

## Proof

- Minimum Task 4 contract rows green for the shard-locked native spine;
  negative controls still detect their bugs.
- `check_sync_points.sh` green.
- No regression: `make runtime-v2-check` twice consecutively.
- `make c-check`, `./check_file_sizes.sh -a` (new files under the ceiling;
  any touched `RV2-DEBT-005` file records its direction),
  `sentrux check runtime/native`, `make check`.
- `make cppcheck` reviewed separately: it still fails only on the known
  pre-existing net const-style findings listed above; Task 4 adds no
  `rt_transport.c` finding.

## Stop Conditions

- The bounded queue needs credit accounting to avoid deadlock even for
  control traffic — stop and re-open the epic's bounded-transport decision
  with evidence.
- The park protocol cannot integrate with the existing shard sleep without
  breaking a `runtime-v2-lock-check` or lifecycle gate — stop and record; do
  not weaken lane assertions.
