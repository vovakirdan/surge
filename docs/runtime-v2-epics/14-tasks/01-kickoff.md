# Epic 14 Task 1: Kickoff — Evidence Pins And Contract Decisions

**Status:** complete (2026-07-10). All kickoff decisions resolved; the
handle-genesis architecture carries the recorded external review verdict.
**Kind:** map/evidence + decisions. No runtime or compiler code changes.

## Starting-State Pins (verify before any later task)

Runtime machinery this epic reuses:

- Immediate execute/reply category: `rt_immediate_on.c` —
  `rt_immediate_on_execute` (caller side, placement resolve + pending +
  reply wait), `rt_immediate_on_dispatch_execute` (destination side,
  bind-then-register with teardown re-check), caller teardown release
  (`rt_immediate_on_release_owned`, hooked in `rt_task_complete.c`
  mark-done). Pending/reply-wait/take-owner discipline:
  `rt_remote_task_pending.c` (listed pendings, `take_owner` matching by
  task id + generation + owner shard), `rt_remote_task_wait.c`
  (register-then-verify park).
- Cancel inheritance: `dispatch_execute_cancel` in
  `rt_remote_task_dispatch.c` (token-validated, no reply-edge contact);
  a cancelled task cannot re-park (`rt_async_poll.c`, `rt_async_yield`
  completes it), so the cancel path is the caller's single resume.
- Local channels — the owner-side machinery the shipped bodies will call:
  `rt_async_channel.c` (`rt_channel_new(capacity)`, `rt_channel_send`,
  recv/async lanes) and `rt_channel_sync.c` (try/compat/blocking/close
  lanes), shared lane protocol in `rt_channel_lane.h`. Channels already
  carry their owner: `rt_channel_owner_shard_id` (`rt_async_channel.c:6`).
- Deadlock-kindness precedents: runtime — `rt_async_poll.c:289` panics
  `"async deadlock"` when the external-await driver has an undone target
  and nothing runnable (deterministic, actionable, release-mode); compiler
  — `SemaLockPotentialDeadlock` (3102) and `SemaTaskLeakInAsync` (3109)
  prove the sema-diagnosis infrastructure exists
  (`internal/diag/codes.go:216-234`).

Compiler surface this epic opens:

- Sema records `CrossingLoweringOnFarHandle` with the anchor destination
  (`CrossingDestinationFarHandle`, `AnchorSymbol`, `OwnerAnchored`) and the
  in-block operation list (`RemoteOps`, `internal/sema/on_crossing.go`).
  Anchor discipline shipped in Epic 11: `SEM3142` (op outside `on`),
  `SEM3150` (unanchored handle in block), `SEM3153` (nested `on`);
  `far TcpConn` is control-only (`on_crossing_capture.go:32` — only
  `close`).
- Guard state: `OnFarHandle` is default-closed in
  `backendSupportsCrossingForm` (`internal/buildpipeline/
  crossing_transport.go`) and `crossingRecordExecutable` has no
  `OnFarHandle` arm (falls to `default: false`); the two-stage guard in
  `on_crossing_check.go` reports the (currently generic) FUT7014.
- Handle-table precedent: the far-task lease registry
  (`rt_remote_task_lease.c`: alloc/find/consume/restore/release-route,
  generation tokens, teardown walks) is the shape the channel handle
  registry mirrors or extends.

## Decision: Anchored Operation Set For The First Vertical

`{send, recv, close}` on `far Channel<T>` — the blocking channel family the
suspend-capable body can express (`internal/sema/nonblocking_check.go:28-30`
classifies exactly these as suspending ops). In-body result shapes follow
LOCAL channel semantics unchanged (the body runs on the owner; kindness rule:
remote must not invent new op semantics): `send` -> the local send result,
`recv` -> the local recv result, `close` -> the local close result; the block
wraps whatever the body `ret`s into `TaskResult<T>` exactly as `on placement`
does. `try_send`/`try_recv` and `far TcpConn` I/O stay out of the first
vertical; `far TcpConn.close` rides along only if it needs zero
channel-specific work (Task 4 decides when the lowering lands).

## Decision: Owner-Side Linearization Point

The linearization point promised by boundary decision 2 is the local channel
lane transition on the channel's owner shard — the same lane protocol
(`rt_channel_lane.h`) that orders owner-local operations today. A shipped
body's `ch.send` enters that lane as an ordinary local operation, so
owner-side FIFO is inherited from the local channel rather than implemented
in transport. The pinning test observes lane order, not transport arrival
order. (This is why the body-shipping lowering makes the FIFO promise cheap:
the alternative per-op design would have had to rebuild this ordering in the
dispatcher.)

## Decision: Self-Deadlock Behavior (boundary decision 5 resolved)

Chosen: **detection with a deterministic, actionable panic in all build
modes**, extending the existing `"async deadlock"` precedent rather than
inventing a new mechanism or accepting a documented hang.

- Shape: when every shard is quiescent (all workers at the idle-park
  boundary, transport queues drained) while at least one execute reply is
  outstanding and its body task is parked on a channel waiter, the runtime
  panics with a message naming the channel, the parked operation, and the
  suspended caller — the remote analog of `rt_async_poll.c:289`.
  Debug builds may add full wait-cycle attribution; the release message is
  already actionable.
- Detection site: the worker idle-park boundary (`rt_worker_turn.c`) — the
  check runs only when a shard is about to park with the executor
  quiescent, so the steady-state cost is zero.
- Rationale per the diagnostics contract (epic decision 8): the shape is
  statically undecidable (consumer topology is dynamic) but dynamically
  observable at quiescence; a silent hang fails the kindness bar; a
  deterministic panic matches how local external-await deadlocks already
  behave.
- The Task 2 reproducer row pins: `on ch { ch.send(v) }` on a full channel
  whose only consumer is the initiating caller -> the panic message, on
  `SURGE_SHARDS=1` and `2`.
- Feasibility bound: if the quiescence check cannot distinguish "deadlocked
  on a channel waiter" from "legitimately idle with a live external
  producer" without cross-shard state walking, the detection narrows to
  debug builds and the release behavior becomes the deterministic
  DESTINATION-side outcome decided at design review — this is the epic's
  first stop condition, not a silent scope cut.

## Decision: Handle Genesis (resolved; external review recorded)

Epic 11 records: "There is no producer of a `far Channel<T>` value except
function parameters" — no runnable program can obtain a handle today. The
external review (Codex pass) verified the load-bearing ground first: L04
sanctions ownership-transfer-on-return for far handles; the far-task lease
registry is the reusable shape; and `SEM3157` rejects only `spawn on ch`
(channel as DESTINATION) — it does not touch a body returning a fresh
channel. Committed architecture:

- **G4 `channel_on` is the headline user story** — shipped in THIS epic:
  `let ch: far Channel<int> = channel_on(dst, cap);` says exactly what
  happens ("create a channel on that shard"). It lowers as thin sugar over
  the G1 primitive. Genesis must not look like an incidental side effect of
  task returns.
- **G1 return-mints is the sanctioned semantic primitive**, with a
  deliberately NOMINAL AND NARROW typing rule: a crossing body may export
  only a FRESHLY-CREATED `Channel<T>` (created in this body, not otherwise
  retained locally — enforced by a freshness/escape check or a dedicated
  internal export operation), and lowering turns that specific result into
  `far Channel<T>`. This is a capability-transfer rule for channels, NOT a
  general `T -> far T` return coercion: arrays, strings, `TcpConn`, and
  user structs do not acquire `far` forms by return, and no "every owned
  return becomes far" precedent is created.
- **G2 capture-mints rejected** (captures flow toward the destination —
  ownership inversion / implicit shard migration). **G3 harness-only
  rejected** as the genesis answer (unreachable user feature; select/scopes
  would inherit the hole); the harness still mints handles for
  runtime-level tests.
- **Token subsystem: one allocator, two lifetimes.** A shared handle-token
  allocator/validator whose token carries `owner_shard + kind + id +
  generation`, where `kind` separates task-lease from channel-handle —
  killing the cross-registry aliasing trap (a far Channel and far Task can
  never resolve to the same record even on id collision). Lifetime models
  stay independent: far-task leases remain one-shot refcounted
  result-transfer records; far-channel entries are live object records,
  closed explicitly, retained while ops are in flight. Refcounts and
  teardown are NOT coupled. Teardown ordering: stop accepting new
  crossings -> drain/cancel in-flight -> invalidate generations -> reclaim.
- **The local-counterparty rule (trap surfaced by review).** A mint whose
  owner shard retains no consumer is meaningless: `channel_new(); ret ch;`
  with the only handle exported leaves nothing draining the owner-side
  channel, and a remote send blocks forever at capacity — the genesis-time
  face of the self-deadlock family. The genesis contract must state what
  holds the owner-side endpoint at mint time; the supported idiom is
  body-creates-channel, spawns a LOCAL owner-side consumer task bound to
  one end, returns the far producer handle. A reproducer row pins the
  no-counterparty shape against the decision-5 detection behavior.

Slice 1.5 (genesis) deliverables: (1) shared typed handle-token mechanism;
(2) G1 fresh-channel-return semantics with the freshness/escape check;
(3) owner-side channel registry + teardown ordering; (4) the
local-counterparty contract with its reproducer; (5) one source-level
create/send/recv e2e; (6) the G4 `channel_on` API shape as G1 sugar.

## Diagnostics Tier Assignment (per epic decision 8)

| Failure | Tier | Code plan |
| --- | --- | --- |
| anchored op outside `on` / unanchored handle / nested `on` | sema (shipped) | SEM3142/3150/3153 unchanged |
| crossing in synchronous context | sema (precision pass, slice 5b) | new SEM code + "make the enclosing function `async`" hint |
| non-crossable payload/capture | sema (precision pass, slice 5b) | new SEM code naming the exact nested field |
| `on ch` on VM/unknown backend | compile, backend-dependent | FUT7014 (message narrowed to the true cause) |
| stale handle / dead owner | runtime | deterministic status, distinct from closed |
| channel closed | runtime | ordinary closed outcome through the reply |
| self-deadlock | runtime | detection panic (above) |

## Sentrux Baselines (committed tree at kickoff)

Root `6183`, `internal` `6518`, `runtime` `5324`, `runtime/native` `5409` —
all rules passing (CLI, committed-tree convention).
