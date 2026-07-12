# Epic 14 Task 4: `on ch` Lowering And The Capability Flip

**Status:** in progress (2026-07-12).
**Kind:** compiler lowering (sema/MIR/LLVM) + capability flip + guard matrix.
**Depends on:** Tasks 2-3 (anchored execute runtime, registry pinning, rows).

## Starting State (surveyed)

- sema already classifies `on far_handle { ... }` into
  `CrossingLoweringOnFarHandle` with the anchor symbol, captures, and
  recorded remote ops (`on_crossing.go` / `on_crossing_capture.go`), but
  `typeFarHandleCall` is a compile-only stub: every anchored method types
  as `nothing`, arguments are typed but not checked against the element
  type, and no method whitelist exists for `far Channel<T>`.
- MIR names the kind but prepares nothing for it: no body poll function,
  no pending slot (`lower_expr_crossing.go`), and
  `crossingUsesPendingRetryState` excludes it.
- LLVM has no emit arm; `backendSupportsCrossingForm` advertises neither
  `OnFarHandle` nor `ChannelCreate` (the genesis e2e runs under a
  test-scoped override).
- Local channel ops in async functions lower as `InstrChanSend` /
  `InstrChanRecv` keyed on `isChannelType(receiver)` with the
  suspend-block protocol (`lower_expr_calls.go`); `close` rides the plain
  intrinsic call path to `rt_channel_close`.

## Design (pinned before code)

1. **Anchored-op surface = local parity.** Inside `on ch { ... }` with
   `ch: far Channel<T>`, the first vertical accepts exactly `send`,
   `recv`, `close` with the local signatures: `send(own T) -> nothing`,
   `recv() -> Option<T>`, `close() -> nothing`. `typeFarHandleCall` gains
   real typing: arg count/type checks against the channel element, real
   result types, and a named rejection for any other channel method.
   `far TcpConn` keeps its control-only whitelist unchanged.
2. **Resolve-once, owner-side, no failure path in the body.** The
   dispatch side already validates the anchor and pins the registry entry
   BEFORE creating the body (Tasks 2-3). It now also resolves the local
   channel then and caches the pointer in the shared pending. Compiled
   bodies fetch it through a new runtime helper (working shape:
   `rt_anchored_channel_for_current_task()` — find the current task's
   bound EXECUTE_ANCHORED pending, return the cached pointer) in a hidden
   prologue local. This honors the pin contract exactly (release during
   the block cannot invalidate the cached pointer — proven by the
   pin-vs-release row) and leaves the body with NO resolve-failure path:
   stale anchors are answered at dispatch, before any body exists.
3. **Body ops lower as local ops on the cached pointer.** Inside the
   anchored body poll function, method calls whose receiver is the anchor
   lower to the existing local machinery — `InstrChanSend` /
   `InstrChanRecv` suspend blocks and the `rt_channel_close` call — with
   the receiver operand substituted by the hidden resolved-channel local.
   The linearization point stays the owner's local channel lane by
   construction; no new channel protocol is introduced.
4. **Caller-side emit mirrors immediate-on.** The LLVM arm for
   `OnFarHandle` follows `emitImmediateOnCrossing` verbatim except the
   destination: the anchor lowers as `ptr` (the caller's heap handle
   token) and the call is `rt_immediate_on_execute_anchored(anchor,
   poll_fn_id, state, pending, kind, bits)` with the same retry/status/
   TaskResult materialization.
5. **Capability flip is two entries.** `backendSupportsCrossingForm`
   (LLVM) adds `CrossingLoweringChannelCreate` (production flip for the
   genesis vertical — its guarded e2e already exists) and
   `CrossingLoweringOnFarHandle`. `crossingRecordExecutable` gains an
   `OnFarHandle` arm with the same capture-verdict rules as
   `OnPlacement` plus the payload-copyable gate, and the anchor itself
   (a far-handle capture) must stay admissible — the far-Task exclusion
   keeps rejecting direct `far Task<T>` captures.
6. **Guard matrix stays deterministic.** VM/unknown backends and sync
   contexts keep FUT-coded rejections for both flipped forms; the matrix
   test grows rows for `on ch` (sync context, VM backend, unknown
   backend, imported module) and keeps `TestChannelOnStaysGuardedOnAllBackends`
   inverted into "guarded everywhere except LLVM".

## Body-Suspension Decision (second opinion, 2026-07-12)

Existing crossing bodies are one-shot poll functions; anchored send/recv
must park mid-body. The fork — synthesize an async function per body and
reuse the async state-machine transform (full machinery) vs restrict the
first vertical — went to an external second opinion (Codex): **restricted
vertical now** (~85%), full machinery as a named debt row implemented
later behind an opaque lowered-body artifact seam (crossing lowering must
never inspect the transform's `{pc, variants}` layout directly).

The vertical-1 rule, strict and structural so re-entry-from-top is sound:

- the anchored operation is the FIRST statement of the block (directly a
  call statement, a `let` initializer, or the `ret` expression — never
  under `if`/loop/`match`), so a parked re-entry re-executes an empty
  prefix;
- exactly ONE anchored operation in the whole body: a second suspension
  point would replay the first op on its re-entry;
- no other suspension inside the body (nested `on` is already SEM3153);
- the post-op suffix runs exactly once (the retry consumes the ack and
  falls through), so ordinary effects after the op are fine;
- the diagnostic names the restriction and the workaround: split into
  multiple `on ch` blocks. `send`-then-`close` is a documented follow-up
  relaxation (close never suspends), not part of the first gate.

Lowering consequence: the park protocol lives in three runtime helpers —
`rt_anchored_channel_send(state, bits)` (yield inside on a false send;
returning means sent), `rt_anchored_channel_recv(state, &bits)` (yield
inside on parked; returns the local recv status), and
`rt_anchored_channel_close()` — over the dispatch-cached channel, so the
compiled body reuses the local channel protocol byte-for-byte and MIR
emits plain calls. A compiler-lowered retry-after-park row is REQUIRED
(the hand-written harness bodies do not prove the compiled path).

## Increments

- **A. sema typing parity** — real `typeFarHandleCall` for channel
  anchors (whitelist, arg checks, result types); unit rows for send/recv/
  close typing, wrong-arg negatives, non-whitelisted method negative.
- **B. runtime pending-cached resolve** — dispatch caches the resolved
  channel in the pending under the existing pin;
  `rt_anchored_channel_for_current_task` helper + harness row proving the
  cached pointer equals the owner-side channel and survives release.
- **C. sema vertical-1 rule + runtime helpers** — the structural
  first-statement/single-op restriction with its friendly diagnostic;
  the three anchored channel helpers in the runtime with harness rows.
- **C2. MIR body lowering** — OnFarHandle prepares body poll fn + pending
  (OnPlacement shape); anchored ops lower as plain calls to the helpers
  (recv materializes Option<T> from the status/bits pair);
  crossingUsesPendingRetryState includes OnFarHandle.
- **D. LLVM emit + flip + matrix** — anchored execute emit arm, builtin
  decls, capability flip for both forms, guard-matrix updates, source
  e2e: `channel_on` + `on ch { send/recv/close }` across
  `SURGE_SHARDS=1,2,8` including owner==caller, wired into the transport
  gate.

## Gates

Per-task rules: `make check`, `c-check`/`c-warnings`/`cppcheck` for the
runtime increment, `runtime-v2-transport-check` + crossing gates for the
flip, golden-update review (guard-matrix churn expected diag-only),
committed-tree Sentrux four-scope comparison at closeout.

Sentrux baselines at this task's start (committed tree, Tasks 2-3
closeout): root `6186` (min_equality breach tracked as RV2-DEBT-029),
`internal` `6509`, `runtime` `5309`, `runtime/native` `5399`.

## Stop Conditions

Inherited from the epic: the dispatcher never blocks; anchored ops never
walk owner state from the caller side; if the body-op rewrite cannot
reuse the local channel suspend protocol unchanged, stop and route to
design review rather than fork the protocol.
