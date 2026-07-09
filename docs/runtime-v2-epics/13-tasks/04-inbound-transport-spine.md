# Epic 13 Task 4: Inbound Transport Spine

**Status:** pending.
**Kind:** native runtime. The spine only — no crossing lowering, no
publication protocol yet.
**Depends on:** Task 3 (tests exist and define done).

## Goal

Add the per-shard inbound transport: bounded queue with a reserved control
lane, an OS-neutral wake abstraction, the seq-cst park protocol, worker-loop
drain points, transport trace counters, and shutdown cleanup — passing every
Task 3 row.

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
- Wake abstraction is OS-neutral in the public shape: pipe fallback is
  acceptable first, `eventfd` is the target Linux implementation behind the
  same interface — the interface must not leak `eventfd` semantics.
- Each message has exactly one owning free path.
- Transport wake writes / elisions / enqueues are new counters, separate from
  net poll counters.
- A shard drains its OWN inbound queue in its worker loop — this is what the
  reply-wait invariant and the N=1 self-crossing row rely on.

## Scope

In: `runtime/native/` new files (suggested: `rt_transport.c/.h` for the
queue+park protocol, `rt_transport_wake.c` for the wake abstraction — final
split decided in-task under the 500-line ceiling), `rt_shard` fields,
worker-loop drain points, trace counters, shutdown path, sync-point
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

### 3. Wake abstraction

`rt_transport_wake` interface: create/destroy, arm-in-pollset, write, drain.
Pipe implementation first; the fd is added to the shard's poll set alongside
`net_poll_wake`. Keep the interface OS-neutral (no eventfd flags in the
public shape).

### 4. Drain points and shutdown

Worker loop drains inbound at turn start and after poll wake; shutdown
enqueues control-lane wake to every shard and joins; destroy tears down the
queue with the owning-free-path rule (assert queue empty or drain-and-free
with counters).

### 5. Counters and debug invariant

Counters: `transport_enqueue`, `transport_wake_writes`,
`transport_wake_elisions`, `transport_control_enqueue`, plus the
PARKED-with-inbound-work debug assertion at safepoints (debug builds).

### 6. Make Task 3 green

Wire the sync-point production sites; run the full Task 3 matrix at
`SURGE_SHARDS=1,2,8`, including negative controls.

## Proof

- Every Task 3 row green; negative controls still detect their bugs.
- `check_sync_points.sh` green.
- No regression: `make runtime-v2-check` twice consecutively.
- `make c-check`, `make cppcheck`, `./check_file_sizes.sh -a` (new files
  under the ceiling; any touched `RV2-DEBT-005` file records its direction),
  `sentrux check runtime/native`, `make check`.

## Stop Conditions

- The bounded queue needs credit accounting to avoid deadlock even for
  control traffic — stop and re-open the epic's bounded-transport decision
  with evidence.
- The park protocol cannot integrate with the existing shard sleep without
  breaking a `runtime-v2-lock-check` or lifecycle gate — stop and record; do
  not weaken lane assertions.
