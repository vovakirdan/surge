# Epic 14 Task 1.5: Handle Genesis

**Status:** complete (2026-07-10). Shipped: the kind-tagged token, the
owner-side registry with teardown ordering, the create message pair with
counters, the caller API, the harness rows (mint/resolve/release + drop
hook, kind aliasing, shutdown sweep, one-shard self-crossing), the
`channel_on` intrinsic with its sema record and FUT7018 guard, the MIR/LLVM
lowering, and the override-gated mint e2e (`SURGE_SHARDS=1,2,8`) wired into
the transport gate as the channel genesis section.

Dispositions of the remaining contract items:

- **Fresh-return as user syntax: stop condition exercised.** The
  freshness/escape check for `ret <channel>` needs whole-function dataflow
  beyond existing sema analyses, and the reply payload path (64-bit
  kind/bits) cannot carry a token without the shared-pending transfer that
  `channel_on` already uses — so per this document's first stop condition,
  `channel_on` IS the shipped producer (its implementation — owner-side
  create, registry mint, token transfer through the shared pending — is
  exactly the sanctioned primitive's semantics), and `ret <fresh channel>`
  as surface syntax goes back to design review for a later task with a
  recorded dataflow plan. The epic decision 9 text is amended accordingly.
- **Handle drop: hook shipped, wiring deferred with the language.** The
  LLVM backend emits no scope-exit drops for ANY owned type today
  (`InstrDrop` is a no-op; heap reclamation is task/heap-cell level), so
  the caller-side token follows the language-wide story rather than
  getting a bespoke mechanism: `rt_far_channel_handle_drop` (release the
  registry entry + free the token, stale-tolerant) exists and is
  harness-proven; the general drop machinery wires to it when it lands.
  Until then tokens are reclaimed with their task's heap cell and registry
  entries at shutdown — bounded per run, recorded.
- **Local-counterparty contract: handed to Tasks 2-3.** The contract text
  lives in epic decision 9; the reproducer (mint with no owner-side
  consumer -> capacity-blocked remote send -> the decision-5 detection
  outcome) requires the anchored send lowering and the detection runtime,
  both Task 2-3 deliverables. The registry hooks the reproducer needs
  (resolve/inflight pinning) shipped here.
**Kind:** runtime handle registry + typing primitive + `channel_on` surface.
**Depends on:** Task 1 (contract: epic decision 9, `01-kickoff.md`).

## Goal

A runnable program can obtain a `far Channel<T>`: the kind-tagged shared
handle token, the owner-side channel registry with its teardown ordering,
the narrow fresh-channel-return primitive, and the `channel_on` producer —
proven by a source-level mint e2e and harness rows.

## Scope Split (recorded up front)

The kickoff's "create/send/recv e2e" splits honestly: this slice delivers
the MINT vertical (create remotely, receive a valid handle, release/teardown
paths) end to end; anchored `send`/`recv` execution is Tasks 3-4 (the
anchored-op lowering), and the combined create/send/recv e2e is Task 4's
flip row. The local-counterparty reproducer also lands with Tasks 2-3: it
pins behavior against the decision-5 detection, which is Task 3 runtime
work — this slice records the contract text and ships the registry hooks the
reproducer needs.

## Plan (test-first per epic rules)

1. Harness rows first (`runtime_v2_pending` behavior suite): mint from a
   non-owner shard -> handle carries `kind=channel`, the destination owner
   shard, a fresh id and generation, and resolves in the owner registry;
   the minted channel works as an ordinary LOCAL channel on the owner
   (driven by the harness); close invalidates the generation (stale token
   thereafter, distinct from closed); a fabricated wrong-kind token (a far
   TASK token presented as a channel) is rejected — the aliasing row;
   shutdown releases every live entry (teardown ordering: stop new ->
   drain -> invalidate -> reclaim); leak audit on the registry.
2. Runtime: `kind` tag in the shared handle token (the `_pad` field of
   `rt_far_task_handle` — zero ABI change; task mints stamp
   task-kind, validators check kind before generation); new
   `rt_far_channel.c` registry (live object records, explicit close,
   in-flight refs — lifetimes independent from task leases per the epic
   decision); `rt_far_channel_create` caller API on the execute/reply
   discipline (caller-allocated handle out-param like `spawn on`;
   destination creates the channel via `rt_channel_new`, registers, binds
   id/generation into the shared pending, replies once).
3. Compiler surface: `channel_on(dst, cap)` prelude signature typed as
   `far Channel<T>` producer; lowering to the create builtin under the
   test-scoped override first; the fresh-channel-return primitive
   (`ret <fresh Channel<T>>` in a crossing body mints) as the sanctioned
   core with its freshness/escape check — `channel_on` lowers through it
   or directly to the create call (Task 1 architecture allows either as
   long as the semantics are the primitive's).
4. Source-level mint e2e (`SURGE_SHARDS=1,2,8`): `channel_on` returns a
   handle; handle fields observable via a follow-up crossing... (bounded by
   what executes pre-Task-4: the e2e asserts mint success + deterministic
   behavior of a second mint + program completion; registry state asserted
   at the harness level).
5. Gates per runtime+compiler task, committed-tree Sentrux comparison,
   NOTES record.

## Stop Conditions

- The fresh-return check cannot distinguish a fresh channel from an escaped
  one at sema without whole-function dataflow beyond existing analyses —
  stop; `channel_on` narrows to the direct create call and the return
  primitive goes back to design review.
- The shared-token kind check would break any existing far-task row — stop;
  the aliasing defense moves to registry-side validation only, recorded.
