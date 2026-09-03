# Epic 23b — Wave D Execution Plan

Containers and local carriers. Written 2026-08-11 against HEAD `f2641713`,
after a ruling-by-ruling pass over the eight standing blockers.

**Statuses last re-established against the tree at `03379549` on 2026-08-28**,
by opening the code each step was supposed to produce rather than by reading the
previous status line. The status table in §4, the per-step record in §4.1 and
the handback in §7 are that pass; everything else in this file is the original
plan and the rulings that shaped it, kept unedited as the record. Between
2026-08-25 (the previous status edit, `afa902a4`) and this pass, D2, D4b, the
D6 tail and one Wave-F lane landed while the table still called two of them
open.

This plan lives IN THE REPOSITORY. Its predecessor
(`23b-wave-d-execution-plan-v2-final.md`) was written into a session scratchpad,
was never committed, and was lost; only fragments survived inside an agent
transcript. That is why this file exists here and not there.

Authoritative scope: `23b-inline-storage-and-typed-carriers.md` §5 "Wave D".
This document adds ordering, entry conditions and evidence; it does not restate
or amend the wave's scope.

## 0. Entry conditions — verified, not assumed

| Condition | Evidence at `f2641713` |
| --- | --- |
| Wave B owner/slot API integrated | `internal/valueops` (Entry/flags/slotRules), `runtime/native/rt_slot_control.c`, generated ABI manifest + `__surge_runtime_abi_typed_carrier_v2_<hash>` sentinel |
| Wave C ordinary storage integrated | Both backend cutovers landed and were integrated at `fb1f9975` |
| A Surge program actually runs in the corpus | `make behaviour-check-all` 0 failures on BOTH backends; `make behaviour-check-mt` green |
| Panic surface ratchets | `internal/panicgate`, 331 raise sites |

The third row is new since the blocker list was written and retires the
"nothing in CI runs a Surge program" blocker. The obligation that came with it:
the full lanes are run at wave milestones, not only the VM lane the hook runs.

## 1. The eight blockers and their rulings

All eight were re-verified against `f2641713` before any ruling was taken. Line
numbers had moved since `fb1f9975`; **no blocker had dissolved**, and two got
sharper. Rulings 1–8 below are owner rulings taken 2026-08-11; they are inputs
to this plan, not proposals inside it.

| # | Blocker | Ruling |
| --- | --- | --- |
| 1 | Agreement test queried the bookkeeping its own subject writes (`requireDropGlue`, `emit_drop_glue.go:122-129`, inserts unconditionally) | Compare **leaf SETS** from the two independent sources, never verdicts — `CarrierDroppable` and `ownsHeap` are designed to disagree on verdicts |
| 2 | `drop_in_place = dropGlueName(id)` binds a NO-OP for a `string`/dyn-array/far/rc-scalar root (`emitDropGlueBody:459-509` has no default arm) | Add the **default arm inside `emitDropGlueBody`**, taking the handle branch `emitDropResultGlueBody:411-425` already uses |
| 3 | "The channel is never freed" is FALSE — `rt_far_channel.c:503` mints, `:224` frees, anchored parking registers pointer-keyed waiters on it | **Reproduce first** with a sync point and a negative control; fix second |
| 4 | D1 pre-splits are uncommittable — `findingKey` carries `Path`, and `TestLiveCarrierRatchetAgainstRepository` (`base_test.go:74`) has no build tag, so it runs inside `make check` → pre-commit hook | **Do not split the files at all.** `findingKey` keeps `Path` |
| 5 | Proposed fix ran `rt_channel_free` under the owner shard lock while `rt_channel_close` kept dereferencing `ch` | Folded into ruling 3 — the reproduction decides the shape |
| 6 | Retirement enumeration wrong both ways: `pop_waiter` is dead code (declared `rt_waiter.h:127`, defined `rt_async_waiter.c:701`, **zero callers**); channel keys never move; the re-arm branch was missing from the list | Folded into ruling 3; the dead symbol goes in its own commit first |
| 7 | `copy_init` — `valueops/flags.go:80` marks it `structural: true` and `entry.go:145-147` says the runtime's generic byte copy satisfies it, while `rt_slot_control.c:42` demands `COPY ⟺ copy_init != NULL` and **no such symbol exists** | **Add the generic byte-copy symbol to the runtime** and bind it. The C-side check stays strict |
| 8 | RV2-DEBT-151 cannot retire as the old plan scoped it — `DEBT.md:138` says in bold the helpers cover FAR and CROSSING too | **Honour DEBT.md**: the whole row retires inside Wave D, no split by locality |

### Blocker 9, found during this pass — the sanitizer gate does not exist

`make runtime-v2-carrier-sanitizer-check` is named as a MANDATORY gate in three
places (§12 twice, `LIVENESS_PROBES.md:42`) and **has no Makefile target**. It
has therefore never run. §12 also says an unavailable mandatory sanitizer gate
blocks closeout and cannot be converted to tooling debt, so the disposition is
determined by the epic itself: the gate is built, in D0. It is listed here
because a plan that scheduled work behind a gate that cannot run would be
repeating the failure this epic already catalogued.

### What re-verification changed

- **Blocker 7 is a different shape than filed.** Not "a fourth callback nobody
  wrote" — a two-sided ABI contradiction. It is latent only because no live
  descriptor reaches `rt_slot_control` yet. It fires on Wave D's first real
  descriptor, which makes it a D0 item and not a late one.
- **Blocker 6 is stronger than filed**: `pop_waiter` is provably dead.
- The right shape for ruling 2 is now 48 lines from the defect, not 15.

## 2. Shape of the wave

D0 is a foundation block: it repairs what the owner migrations would otherwise
build on top of, and every item in it is reachable today without touching a
single carrier owner. No owner migrates until D0 is integrated.

D1 onward are owner migrations. Per §5, **every owner migration deletes its old
fields and dispatch path in the same commit; there is no adapter milestone.**

Per ruling 4 there is no file-splitting commit anywhere in this plan.

## 3. D0 — foundations (no owner migrates during D0)

### D0.1 — delete the dead waiter symbol

Entry: none.

Delete `pop_waiter` (declaration `rt_waiter.h:127`, definition
`rt_async_waiter.c:701`) and re-derive the true list of waiter-store mutation
points, **including the re-arm branch** in `rt_channel_lane.h` that the old
enumeration missed and that would double-retain on every absorbed wake.

Evidence: `make runtime-v2-waiter-check`, `make check`. The re-derived mutation
list is recorded in the commit message, because D0.3 depends on it being right.

### D0.2 — the drop-glue default arm (ruling 2)

Entry: none.

`emitDropGlueBody` gains a default arm routing a non-composite root through the
handle branch. A `string`, dynamic array, far handle or rc-scalar root stops
emitting `entry → ret void`.

Negative control, mandatory: a build with the default arm removed must fail the
D0.3 agreement test. A green test against an unbroken subject proves nothing —
that is the exact defect ruling 1 exists to prevent.

Evidence: `make runtime-v2-carrier-check` ×2, `make golden-check` ×2,
`make behaviour-check-all`, `make check`.

### D0.3 — the agreement test, rebuilt on leaf sets (ruling 1)

Entry: D0.2.

The test compares the SET of owning leaves `CarrierDroppable` reaches against
the set `ownsHeap` reaches, per type. It must not read `dropGlueNeeded` or any
other bookkeeping its subject writes.

Second leg, from ruling 1's confirmation: no droppable type may receive a glue
body that is `entry → ret void`.

Evidence: both legs fail against the D0.2 negative-control build. Recorded as a
pass/fail pair, not as a green run.

### D0.4 — the generic copy symbol (ruling 7)

Entry: none.

Add the runtime's generic byte-copy `rt_value_copy_init_fn` driven by
`rt_value_layout`, and bind it wherever a descriptor sets `RT_VALUE_FLAG_COPY`.
`rt_slot_control.c:42` keeps demanding `copy_init != NULL`; `valueops`'
`structural: true` comment becomes true instead of aspirational.

Negative control: a descriptor that sets COPY with a null `copy_init` is
refused by `rt_slot_control`, named.

Evidence: `make runtime-v2-slot-control-check`,
`make runtime-v2-abi-manifest-check`, `make check`.

### D0.5 — build the sanitizer gate (blocker 9)

Entry: none.

Add `runtime-v2-carrier-sanitizer-check`: Valgrind, ASan/UBSan and TSan
availability checked FIRST, then the focused carrier rows with skip-on-missing
disabled. Per §12 any skip or unavailable tool fails the target.

Evidence: the target fails when a tool is hidden from `PATH`, and passes on the
reference `x86_64-linux-gnu` runner. Proven by breaking it, per
`gates-can-be-green-without-running`.

### D0.6 — reproduce the freed-channel waiter defect (ruling 3, covering 5 and 6)

Entry: D0.1 (the mutation list must be correct first).

A deterministic sync point that parks an anchored waiter keyed by a channel
pointer, then drives `rt_far_channel.c:224`'s free, then delivers. Plus a
**negative control** build that reproduces the old behaviour, so the sync point
is shown to be able to fail.

A timeout-only test does not satisfy this step (§7 of the epic), and neither
does a rate of zero — state what frequency the run could have excluded.

Evidence: `make runtime-v2-syncpoint-check`, `make runtime-v2-waiter-check`.

### D0.7 — fix it

Entry: D0.6 red on the negative control and red on the unfixed tree.

The reproduction decides between retiring waiter entries at close versus at
free. Two constraints the fix must satisfy regardless of which it picks:

- `rt_channel_close` must not dereference `ch` after anything frees it;
- no generated or user operation may run under the owner lock (§8 P2, and the
  matrix row "callback reentry").

Evidence: D0.6 flips to green; the negative control STAYS red;
`make runtime-v2-lock-check`, `make runtime-v2-carrier-sanitizer-check`,
`make behaviour-check-all`, `make behaviour-check-mt`.

### D0 exit condition

All of D0.1–D0.7 integrated on one tree; `make check`,
`make behaviour-check-all`, `make behaviour-check-mt`,
`make runtime-v2-carrier-check` ×2, `make golden-check` ×2 green together, on
the integrated tree and not per-branch.

## 4. D1+ — owner migrations

Ordering is by dependency, not by size. Each step deletes the old fields and
dispatch path in the same commit.

Status column read at `03379549`, 2026-08-28. **CODE COMPLETE** means the step's
storage flip is on the tree and wired; it does NOT mean the step's evidence run
has been recorded. **CLOSED** means both.

| Step | Owner | Status at `03379549` | Entry condition |
| --- | --- | --- | --- |
| D1 | Fixed arrays and dynamic array element buffers | **CLOSED** | D0 |
| D2 | Map key/value entries — insert, rehash, replace, remove, failed insert, teardown | **CODE COMPLETE**; bench re-measure and the two parity rows owed (RV2-DEBT-156/157, gated on 174) | D1 (element storage first) |
| D3 | Buffered/unbuffered channel send/receive and waiter mailboxes | **CLOSED 2026-08-24.** D3b (the channel's own lifetime) is a follow-on: C0 landed, C1 and C3 not started | D0.7 (the waiter fix is a precondition, not a parallel task) |
| D4 | Task canonical result/resume and cloned result entitlements | **D4a CLOSED 2026-08-25. D4b CODE COMPLETE 2026-08-26** on both lanes; P3's four deterministic rows are complete 2026-08-28 and recorded below, its rollback failpoint (RV2-DEBT-304) and child-process controls (RV2-DEBT-305) are not | D0; may run beside D3 only once their production files do not overlap |
| D5 | REMOTE select only — `rt_far_channel_select.c`. Local `select` moved into D3 by the 2026-08-19 ruling below | **CLOSED 2026-08-25** | D3 |
| D6 | Blocking captures/results and every cancellation timing | **CODE COMPLETE.** Results 2026-08-25; captures a-1..a-4 2026-08-26..28; RV2-DEBT-080 stays Open pending the lead's green run of the rows a-3 and a-4 added | D3, D4 |
| D7 | Async frames, captures, polling, wake, normal/shutdown drains | **STATE CARRIAGE CLOSED 2026-08-25** (numeric drop dispatch gone). **The frame itself is NOT STARTED**: 18 live `suspension-frame-owner` carriers, RV2-DEBT-179 Open | D4 |
| D8 | RV2-DEBT-151 retirement — local **and** FAR **and** CROSSING (ruling 8) | **CLOSED 2026-09-01.** Native remains at `llvm-erased-word-bridge=0`. The VM interval allocator and all twelve callers are gone; channel ring/park/resume, task result, and select owners hold exact typed slots addressed by generation-checked metadata-only claims. | D1–D7 |

Worktree rule, from §5: task/channel/select/blocking may be separate worktrees
**only after** the shared owner/slot API is integrated and their production
files do not overlap. Subagent worktrees have twice come up on the wrong
lineage — the base commit is a required field in any worktree handoff.

## 4.1 What the tree says, step by step — established 2026-08-28

The instrument that answers most of this in one number is the carrier scanner
itself, run live against `03379549`: **83 findings against a frozen base census
of 626** (as counted that day; the manifest's frozen base is 683 on
`7df10725` -- corrected 2026-09-03, Wave F F3), and `go test ./internal/carriergate -run
'^TestLiveCarrierRatchetAgainstRepository$'` is green, so nothing live is
outside the manifest's legacy-plus-migration allowance. Per category, live
against base:

| category | base | live | reads as |
| --- | --- | --- | --- |
| `vm-boxed-composite-kind` | 74 | **0** | D1's VM half |
| `llvm-composite-to-ptr` | 5 | **0** | D1's native half |
| `untyped-capture-state` | 15 | **0** | D6 captures |
| `vm-async-any-carrier` | 23 | 2 | both in `internal/asyncrt/timer.go`, `heap.Interface` |
| `llvm-erased-word-bridge` | 25 | **0** (was 3) | D8's adopt leg, retired 2026-08-29 |
| `llvm-pointer-word-ir` | 3 | 1 (was 3) | `emit_term.go`'s allowed fixnum constant, alone |
| `vm-universal-owner` | 13 | 7 | VM `Value` frame slots |
| `composite-box-marker` | 54 | 8 | clone/drop glue naming plus `cloneValueComposite` (RV2-DEBT-246) |
| `native-word-carrier` | 85 | 10 | `rt_far_channel*`, `rt_remote_task_*` — Wave E |
| `numeric-drop-dispatch` | 188 | 13 | `rt_far_channel*` and the crossing emitters — Wave E |
| `native-payload-bits` | 134 | 19 | `rt_remote_task_*` only — Wave E |
| `suspension-frame-owner` | 7 (+12 migration) | **18** | D7's tail, untouched |

**D1 — CLOSED.** The VM half is `08c0bc56` (the storage) and `d6ebe0ac` (a
dynamic array's elements are a typed run); `Arr []Value` is gone from
`internal/vm/object.go`, and `internal/vm/array_storage_internal_test.go:363`
records the retirement. Natively an array header is `{len, cap, data}` with
`cap` counted in ELEMENTS and the data run sized `cap * stride`
(`runtime/native/rt_array_internal.h:8-17`); every entry point takes
`elem_stride` and `elem_align` (`rt.h:24-40`) and the compiler walks the
elements. Both of D1's carrier categories read live zero.

**D2 — CODE COMPLETE, not closed.** `2ba2e0cf` · `01579589` · `429f8821` ·
`97351ecf`, VM half `807bf541`. `SurgeMapEntry` does not exist anywhere in the
tree; `rt_map` holds `key_ops`/`value_ops` and two typed runs in one allocation
(`runtime/native/rt_map.c:52-53,107-118,149-153`), growth moves through
`rt_value_move_init_detached` (`:234-236`), and `rt_map_new` takes both
descriptors (`rt.h:403`). **The section this replaces is history**: DEBT-158's
positional-invalidation defect and RV2-DEBT-172 were both closed 2026-08-19
through the borrow rule (SEM3018), and the map lane confirmed it. What is still
owed is measurement, not code: RV2-DEBT-156's bench clause and RV2-DEBT-157's
parity row for a heap-owning value, both blocked behind RV2-DEBT-174 — the
carrier bench cannot run against the frozen `epic_base` compiler at all since
the `Channel::<T>::new` migration, and re-capturing the digest-frozen manifest
is the owner's call. Also owed and filed: RV2-DEBT-250 (linear `map_find`),
251 (`rt_key_ops` unused), 252 (no sanitizer stand, no owning-key bench row).

**D4b — CODE COMPLETE on both lanes.** Native `12e93f33` · `5bda5efd` (plus
`6d7a0ee8`, `f749b7d3`, `421df648`); VM `210c206b` · `d4ead546` · `f0db0bd7` ·
`3ca25486`. `runtime/native/rt_task_entitlement.h` carries exactly what §10
describes — `live`, `claimed`, a reserved `mover`, atomic `clone_readers`,
`move_waiting`, `moved`, and the installed `duplicate` recipe — with the six
take modes including `WAIT` and `REFUSED`; `internal/vm/task_entitlement.go` is
the VM's. `TestRuntimeV2TaskCohortCostsOneDuplicationPerExtraHandle` is the
`N-1` row. The three deterministic sync-point rows that were missing on
2026-08-28 are on the tree — see **P3 IS CLOSED** below. Filed and open beside
it: RV2-DEBT-246, 247, 248, 249.

### P3 — THE FOUR DETERMINISTIC ROWS ARE COMPLETE, 2026-08-28

The three rows this vertical was short landed together with the hooks they need.
Before them the tree carried only the clone-reader-versus-last-await pair
(`SP_AWAIT_AFTER_INCREMENT`, `SP_AWAIT_BEFORE_DONECV_WAIT`,
`rt_async_task.c`), and the enum declared nothing for the other three; that was
checked against the whole of `runtime/native/rt_sync_point.h` twice, once when
the gap was recorded and once before the rows were built.

WHAT THE ROWS ARE. Three new hooks, each named for the behaviour it holds open
and declared in both `rt_sync_point.h` and the `rt_sp_name` table:

- `SP_CLONE_READER_OUT_OF_LOCK` (`rt_task_entitlement.c`, at the end of
  `rt_task_entitlement_begin_take`) — a take has been decided CLONE, the reader
  is counted into `clone_readers`, the owner shard lock is gone, and the
  duplication reads the canonical value where it lies. It is the only instant at
  which a claim is OUT, which is why two rows race against it;
- `SP_CANCEL_AT_COMMITTED_RESULT` (`rt_task_complete.c`, in `cancel_task`) — a
  cancel arriving at a task already DONE whose result slot still holds a
  published value. It is armed by nothing; its reached count is what proves the
  cancel row is not vacuous;
- `SP_RESULT_CAPABILITY_BEFORE_MATCH` (`rt_task_result.c`, in
  `rt_task_result_matches`) — a result capability resolved to a live task,
  immediately before the slot is asked whether it still holds the occupant that
  capability was minted for.

The stands are `internal/vm/runtime_v2_task_entitlement_syncpoint_test.go` and
`runtime_v2_task_entitlement_stand_test.go`, in the lifecycle harness. They are
the first lifecycle stands whose task result OWNS something: every other one
binds the opaque machine word, and a word's take is a COPY that leaves the slot
alone, so those stands never reach the entitlement machinery at all.

THE THREE ROWS, AND THE GUARD EACH NEGATIVE CONTROL REMOVES:

- **shutdown versus a claimed clone.** A clone reader is held out of lock; the
  executor is told to stop and a sibling entitlement lets go. Positive:
  `canonical_drops=0` at the window, then `duplications=1 drops=2
  double_drops=0` — the reader is served a whole value and the last asker moves
  the canonical. `RV2_SHUTDOWN_UNPINNED_CANONICAL_NEGATIVE_CONTROL` reads
  "shutdown drops any canonical result" literally, dropping it on the first
  release after shutdown however many askers still hold the task; it reports
  `canonical_drops=1` at the same window and fails with "the canonical result
  was destroyed while a claimed clone reader was still out";
- **cancel versus `READY`.** The same window, with a cancel issued through a
  sibling handle. Positive: `at_committed_result=1 canonical_drops=0`, then
  `duplications=2 drops=3 double_drops=0` — a cohort of three, every
  entitlement served its own value, nothing answered Cancelled.
  `RV2_CANCEL_REVOKES_COMMITTED_RESULT_NEGATIVE_CONTROL` lets the cancel empty
  the slot instead; it reports `canonical_drops=1` and fails with "a cancel
  revoked a result the task had already committed, while a claimed clone reader
  was still out";
- **stale-generation late publication.** A capability is minted for one occupant
  of a result slot; the holder is held at the match and the slot is then
  destroyed, rebound and refilled underneath it. Positive: `rebind_drops=1
  late_taken=0 second_occupant_ready=1`, and each occupant destroyed exactly
  once. `RV2_STALE_RESULT_GENERATION_NEGATIVE_CONTROL` drops the generation from
  the comparison, leaving the id and "a value is there" to answer; it reports
  `late_taken=1 second_occupant_ready=0` and fails with "a capability minted for
  one occupant was spent on the next one in the same storage".

Every control leaves the window itself intact and changes exactly one guard, so
none of them can pass by removing the ordering the proof is built on. Each fails
at its own window in seconds with its own sentence; none times out.

WHERE THEY RUN. `make runtime-v2-lifecycle-check` grew a third recipe line for
the six rows, beside the stands the other lifecycle windows already run under.
`./check_sync_points.sh` (the `runtime-v2-syncpoint-check` gate) carries the
three new window/file pairs, so a hook declared without its `rt_sp_name` row, or
called outside its window file, is refused.

WHAT ANSWERS P3'S NAMED CRITERIA. The four deterministic rows §6 of the epic
requires now exist: the out-of-lock clone reader versus sibling drop/last-await
pair was already there, and the three above are the rest. The cohort arithmetic
— `N-1` duplications plus one reserved final move — is
`TestRuntimeV2TaskCohortCostsOneDuplicationPerExtraHandle` for the compiled
program and is re-read inside each of the two entitlement rows for the native
cohort. "Cancel remains task-global across entitlements; no clone failure is
translated into entitlement-local `Cancelled`" is the cancel row's own
assertion, on all three handles.

WHAT IS STILL OWED, AND WHY THE HEADING SAYS "THE FOUR ROWS" AND NOT "CLOSED".
§6 asks P3 for two things beside its deterministic rows, and neither is built:

- **the generated `ValueOps` rollback failpoint** — "returns after initializing
  one child to prove rollback", answering the success bullet about a returned
  internal status restoring the destination to `EMPTY`. It cannot be built
  against today's ABI at all: `rt_value_clone_init_fn` is
  `void (*)(void* dst, const void* src)`
  (`runtime/native/rt_typed_carrier_abi.generated.h`), and §3 of the storage
  model says "only an operation that returns an explicit internal status is
  locally rollback-capable". The duplication path has no status slot, so there
  is nothing for a failpoint to return and nothing for the take to roll back
  from. Making it fallible is an ABI change on the clone slot, which is Wave B's
  surface and not a row this lane may add on its own. **RV2-DEBT-304**;
- **the two bounded child-process controls** — a user `__clone` panic following
  the terminal `panic -> exit(Error)` path and never being observed as
  `Cancelled`, and a deterministic allocator `NULL` proving no result
  publication, no `Cancelled` and no retry spin. Both are stand work rather than
  ABI work, and both need a child process the lifecycle harness does not have
  today. **RV2-DEBT-305**.

Also owed and not this worktree's: the gate wall-time run. These rows have been
run singly and as the gate's own recipe line here; the epic's closeout evidence
is the lead's machine.

**D6 — CODE COMPLETE, RV2-DEBT-080 still Open by its own terms.** Results
landed `66c2f156`: `rt_blocking_submit(fn_id, state, state_type_id,
result_type_id)` (`rt.h:340-348`) binds ONE descriptor to the job's cell and the
awaiting poll's. Captures landed as a-1 `dcdcb2da`, a-2 `0a3fa567` (the job's
state is an `rt_value_cell` adopted from the compiler's block,
`rt_async_blocking.c:404`), a-3 `2a79f345` (`dropObligationsSuppressed` is
deleted; only `blockingDepth`, which answers a different question, survives) and
a-4 `2f67cc9b` — the §7 cancellation rows the two negative-control toggles had
been asserted against with nothing observing them now exist as
`TestRuntimeV2LifecycleDebt080*`
(`internal/vm/runtime_v2_blocking_cancel_lifecycle_test.go`), each with its
negative control naming the sentence it must fail with. The `untyped-capture-
state` category reads live zero. The row's closure condition is the lead's green
run of a-3's four valgrind rows and a-4's lifecycle rows, and that run is not
recorded.

**Which step owns the cancellation-ANSWER family.** This table's D6 row says
"and every cancellation timing", and a-4 delivered every cancellation timing of
the BLOCKING owner, which is how the epic's §5 bullet reads it. The separate
family — a task committing `Success` after a cancel (RV2-DEBT-261, 263, 265,
291) — is filed against D4b's cancel rows in `NOTES.md` and is tracked there,
not here. It is not closed: 261 and 263 were reopened 2026-08-27 when
`runtime-v2-lifecycle-check` went red on the dedicated machine at
`TestRuntimeV2FailfastJoinAnswersCancelled/llvm/threads-4`, and 400 pinned runs
established that RV2-DEBT-265 is not the window either (3 red of 200 before, 1
of 200 after). A fourth window is open at roughly half a percent, under the
gate and not under the row run alone.

**D7 — the frame is NOT STARTED.** What closed on 2026-08-25 (`afa902a4`) is
state CARRIAGE: the three generated dispatch tables are gone from production
(`__surge_drop_call`, `__surge_drop_result_call`,
`__surge_drop_abandoned_state_call` survive only inside C test stands), and the
crossings carry TYPE ids. The frame itself is untouched. Compiled code still
reserves it (`emitRuntimeOwnedStorage`), releases it
(`mir.AsyncStateFreeBuiltin`, `emitAsyncStateFreeIntrinsic`,
`emitSuspensionFrameReleaseBody`) and hands the runtime a bare address plus a
type id (`rt_async_poll.c`, `rt_task_complete.c`, `abandoned_state` +
`abandoned_state_type_id`) — 18 live carriers in that category. §11 of the
storage model is the target and none of it holds: there is no compiler-generated
frame/state descriptor table, no per-suspension-state resume type and slot, and
no state generation a producer must match before initializing. RV2-DEBT-179 —
two emitter sites needing OPPOSITE reclamation with nothing in the frame able to
tell them apart — is the same gap named from the defect side, and is Open.

**D8 — CLOSED 2026-09-01.** The native helper set remains retired and
`llvm-erased-word-bridge` remains zero. The VM half now deletes
`internal/vm/transport_storage.go` and migrates all twelve production callers.
The generic executor carries only a metadata claim; exact bytes live in a
homogeneous channel ring/park/resume, task-result, or select owner region. A
consumer validates owner identity, owner generation, owner-local region, slot
generation, role, and park sequence, then moves directly into caller-owned
exact storage before making the source reusable. No composite `Value` alias
escapes, scalar/handle values need no per-value allocation, and slot-generation
exhaustion terminally poisons only the quiescent slot rather than wrapping into
an ABA-equivalent claim. RV2-DEBT-151 is therefore closed. The far runtime's
own word carriers and suspension-frame ownership remain Wave E / RV2-DEBT-179
work; D8 does not reclassify them.

### D3 IS CLOSED — 2026-08-24

The step landed as `0a3c1882` (the storage flip, parked red) plus four commits
that finished it: `c3780ba5` (FIFO admission and the staged slot's single
owner), `fbde5c51` (a legacy file kept at its size), `2fe5fb7b` (the park pool's
bookkeeping under the owner's lock, the emitter's alignment for a composite
element, and the sanitizer rows), `24f0988f` (the changed-C scan builds a stand
the way its harness does).

WHAT IT CARRIES NOW. `rt_channel` holds one `const rt_value_ops*` plus the
element's type id, a `rt_typed_fifo` ring at the element's own stride and a
`rt_park_pool`, in one allocation. Every entry point takes storage rather than a
word. `rt_task.resume_bits` is gone; a task carries a park token. Local select
stages into storage of its own. The box on the channel boundary is gone,
`Channel<nothing>` costs one byte per cell, and the word-shaped carriers that
remain belong to far (D5), task result (D4), blocking (D6) and map.

P2 CLOSES WITH IT. Its four success criteria, and what answers each:

- exact alignment and one channel-level descriptor — `rt_channel.ops`, and
  `emitChannelPayloadSlot` reading a byte run's alignment from the layout
  registry rather than from its spelling;
- no per-element box — measured: `rt_alloc(16,8)` + memcpy + ptrtoint became
  `rt_channel_send_blocking(ptr %ch, ptr %l5)`;
- generated clone/drop outside the channel lock — `rt_value_*_detached` fails
  closed on a held lane, and it caught two violations during the step;
- every outcome moves or drops once under Valgrind/ASan/TSan —
  `TestRuntimeV2ChannelOwnedElement*` drives a heap-owning element through
  buffered, unbuffered, parked sender and receiver, a full buffer, close and a
  cancelled sender, plain and under ASan+UBSan and TSan, wired into
  `runtime-v2-carrier-sanitizer-check`; the heap gate's valgrind rows cover the
  select payload paths.

EVIDENCE ON THE INTEGRATED TREE. 19 of 21 `runtime-v2-*` gates green;
`ownership-check` and `carrier-check` red with failure text byte-identical to
the base commit (24 issues and `blocking-scalar: 213 != 278` respectively) and
sanctioned. `make check` 0, `make golden-check` 0 with no corpus drift,
`make behaviour-check-mt` 0, `make behaviour-check-all` red on
`string_from_bytes_invalid_utf8/llvm` alone, which fails identically at the
base. The tagged `internal/vm` suite: 12 failures here and 12 at the base, the
sets differing by one load-flaky row each way.

### The record of why it was blocked — owner ruling 2026-08-21

The block below is kept as the record of why the step stopped, and of what had
to be true before it could start. Both conditions are now met, so it no longer
holds:

- **The generation gap is closed.** The sentence that follows — "emits no
  `rt_value_ops` constructor whatsoever" — was true on 2026-08-20 and is false
  now. The backend emits a descriptor per registry type with a per-type
  `move_init`, the real `rt_slot_control_init` admits them, and the mandatory
  `plan_cross` is filled by a module-local trap proven terminal by calling it
  (RV2-DEBT-230, RV2-DEBT-232).
- **The drop the ring drain needs is in the descriptor.** RV2-DEBT-227 un-staged
  DROPPABLE, and a descriptor now carries a real `drop_in_place`. The number
  `rt_channel_new` receives is the ELEMENT's type id, not the channel's, so what
  D3 replaces is the numeric dispatch over an element the descriptor already
  describes. Reclaiming the channel VALUE is RV2-DEBT-155 and is not a
  precondition for this step — that one waits on the channel becoming an RC
  handle, which is a language-surface decision of its own.

What remains staged, and what D3 therefore may not assume: `trace`,
`cross_move_init` and `cross_clone_init` are still filled nowhere, and carriers
whose leaf this backend cannot reclaim — far leases and opaque runtime resources
— get no descriptor at all rather than a drop bit over an empty body.

### The original ruling, kept as the record — owner ruling 2026-08-20

D3 was opened, its code read, and the step stopped before any line was written.
The reason is not that the step is large. **Its entry condition was never met.**

Wave B items 3 and 5 require the compiler to GENERATE the concrete
move/copy/clone/drop/trace/cross operations and the read-only cross plans, and
the wave is declared an integration dependency that "must land before parallel
owner migrations branch from it". P2's entry condition names the shared Wave B
owner/slot API as integrated. Measured 2026-08-20: `internal/backend/llvm/`
emits no `rt_value_ops` constructor whatsoever - the single mention is the
record's SHAPE in `typed_carrier_v2.generated.go` - while
`rt_slot_operations_preflight` refuses any descriptor whose `move_init` or
`plan_cross` is null (`rt_slot_control.c:42`). No typed owner is reachable from
here.

**Order of work is therefore: close the Wave B generation gap first, then P2.**

Three contracts were added ahead of the code, because without them the
implementation is a choice among bad options rather than a target:

- **The mailbox carries control only.** `resume_bits`, `taken_payload` and any
  other word that a payload actually travels through are removed as storage. A
  task keeps a resume SLOT - typed - and the scheduler carries only "a value of
  this generation is ready". The ownership path is: sender staging slot → ring
  slot (buffered) or rendezvous slot (unbuffered) → receiver resume slot; and
  for select, channel slot → the select operation's own typed staging → result
  destination. This dissolves the abort the fail-closed lane predicate would
  otherwise produce, not by unlocking around the old algorithm but by replacing
  the algorithm with claim → unlock → value op → commit.
  NOTE, measured: the payload lives in FOUR places today, not three. A parked
  SENDER also holds its value in its own `resume_bits`, and the receiver takes
  it straight from there (`rt_channel_sync.c`, `*out_bits =
  sender->resume_bits`), so the sender needs a staging slot too.
- **Channel lifetime is reference counted.** `Channel<T>` stays `@copy` at the
  surface; the runtime object gets deterministic retain/release, and the last
  release drops the payloads it still owns. `close` is NOT `destroy`: a closed
  channel is still drained, because `recv` yields `nothing` only when closed AND
  empty - which is what the implementation already does
  (`if (ch->closed) return 2;` runs last, after the buffer and after the parked
  sender). Written into `RUNTIME_V2.md` §7, which had been silent on channel
  lifetime entirely.
- **A slot is not a control block.** One descriptor per homogeneous owner; a
  one-byte `rt_slot_header` per element. A per-element `rt_slot_control` (144
  bytes measured) is a representation choice this design refuses, and a
  zero-sized payload keeps its lifecycle with no storage at all.

Two things are explicitly NOT D3's to fix, and neither may be repaired silently
inside it: `rt_slot_operations_preflight` demanding cross capability from a
purely local owner, and `Channel<T>::new` having no native backend case at all
(`emitChannelIntrinsic` knows only `make_channel`, `close`, `send`, `recv`,
`try_send`, `try_recv`).

The scope findings below stand and feed the eventual step: the census under-
scopes by roughly 41 rows, section 8 P2 is violated on at least four routes
rather than the one named, and the "485 effective against a 500 ceiling" belongs
to `rt_async_select.c` (measured 484), not to `rt_far_channel_select.c` (348).

### D3 absorbs LOCAL select — owner ruling 2026-08-19

`rt_async_select.c` takes control at `:244`, calls both channel cores inside it
(`rt_channel_try_recv_status_locked` at `:333`,
`rt_channel_try_send_status_locked` at `:345`), releases at `:401` and carries a
raw `taken_payload` across the release. Changing the cores' signatures breaks
select, and §5 forbids an adapter, so the two cannot land apart.

**Local select is therefore rewritten inside D3's commit.**
`rt_far_channel_select.c` — 57 census rows — stays D5. The file is 485 effective
against a 500 ceiling, so the arm scan is extracted in the same commit.

Three further rulings taken with it:

- **§8 P2 is already broken and D3 fixes it.** `mark_done` holds control from
  `rt_task_complete.c:200` to `:263` and reaches `rt_channel_free` through
  `rt_remote_task_completion.c:54` → `rt_far_channel.c:194` → `:197` → `:224`;
  `unlock_then_reclaim` releases only the far-channel mutex, so the compiled
  glue at `rt_async_channel.c:69` runs under control TODAY. D3 hoists the
  reclaim out and makes `rt_channel_free` assert, fail-closed, that neither
  control nor a shard is held — which puts `rt_task_complete.c` and
  `rt_remote_task_completion.c` in D3's scope.
- **Descriptors come from the BACKEND.** `internal/valueops` cannot express
  `drop_in_place` — `FlagDroppable` is `staged: true`
  (`internal/valueops/flags.go:109`) — so D3 emits `rt_value_ops` from the
  layout registry and the valueops route becomes its own row.
- **D3 closes both Wave B gaps itself**: the missing detached dispatch helpers
  for `move_init` and `drop_in_place` (only `rt_value_copy_init` exists,
  `rt_value_ops.h:39`), and the missing sentence classifying a `STALE` returned
  by a CLAIM rather than a commit (`rt_slot_control.h:170-172`).

`Channel<nothing>` is NOT an open question: it works today on both lanes and
backs `Mutex`, `Condition` and `Semaphore` (`core/sync.sg:15,53,94`), so
zero-sized payload handling is a requirement of the step.

### D3's native half is Wave B's FIRST production adopter

Measured 2026-08-19 while closing D3's VM half. The scope divides cleanly and
the division matters, because the census makes D3 look twice its size:

| File | Census rows | Step |
| --- | --- | --- |
| `runtime/native/rt_async_channel.c` | 37 | D3 |
| `runtime/native/rt_channel_sync.c` | 21 | D3 |
| `runtime/native/rt_channel_lane.h` | 11 | D3 |
| `internal/backend/llvm/emit_channel.go` | 9 | D3 |
| `runtime/native/rt_far_channel.c` / `.h` | 13 | D3, remote half |
| `runtime/native/rt_far_channel_select.c` | 57 | **D5**, not D3 |

So D3's local core is ~78 rows, comparable to D1's 69 — not the 158 a search
for "channel" returns.

**The fact that shapes the work: `rt_slot_control` has no production consumer.**
Every caller of `rt_slot_control_init` / `rt_slot_publish_initial_locked` today
is a test harness under `internal/vm/testdata/`. Wave B is described as "an
integration dependency for all erased runtime owners" and D3 is where that
dependency is first paid, so the step is a first-of-kind integration rather
than a mechanical retyping.

Two constraints meet in it and they are the design, not a detail:

- the channel buffer is a RING (`buf_head` + `buf_len`), while slot control is
  per-slot with generations and an explicit lifecycle;
- §8 P2 forbids any generated or user operation under the owner lock, and a
  send runs under exactly that lock. The claim/publish split
  (`rt_slot_claim_exclusive_locked` / `rt_slot_publish_initial_locked`) exists
  for this, and D3 is what proves it fits.

Per §5 this lands whole: there is no adapter milestone, so a typed buffer
behind a `value_bits` API is not an intermediate state this plan allows.

### D8 is a deletion step, not a migration

Both helper sets are deleted rather than deprecated, and the transport
round-trip test family keeps passing against the inline path with no copy at the
boundary. Removing the `ownership-allowlist:` marker TEXT is part of retiring an
allowance — a bare row id left behind keeps the bidirectional gate unsatisfied.

## 5. Proving verticals

P2 (typed channel owner) closes with D3. P3 (local typed task result and clone
entitlement) closes with D4. Both entry conditions — P1 plus the shared Wave B
owner/slot API — are met today. Success criteria are §6 of the epic and are not
restated here.

P4 and P5 are Wave E and Wave F respectively and have no edge into this plan.

**Status 2026-08-28.** P2 was recorded closed with D3 (§4's D3 block lists its
four criteria and what answers each). P3's record is now written the same way,
in §4.1 under D4b: its four deterministic sync-point rows are complete — the
three that did not exist that morning landed with the hooks they need, each with
its own negative control and each wired into `runtime-v2-lifecycle-check`. P3 is
still not CLOSED, and the record says exactly what is missing rather than
leaving it to be rediscovered: the generated `ValueOps` rollback failpoint
(RV2-DEBT-304, blocked on the clone slot being `void`) and the two bounded
child-process controls (RV2-DEBT-305). P5 landed independently on 2026-08-26
(Wave F's diagnostic half, `9fe013eb..8a3c7eb2`), which does not change this
plan's edges.

## 6. Closeout evidence

The §12 command list in full, on the integrated tree, plus:

- `make behaviour-check-all` and `make behaviour-check-mt`, both lanes, at the
  D0 exit and again at wave close — not the VM lane alone;
- `make runtime-v2-carrier-sanitizer-check` (which exists as of D0.5);
- every sync point recorded as a positive AND a negative control;
- `make runtime-v2-file-size-check EPIC_BASE=7df10725e001ddf915d536aa58f880bd7e04aafd`.

Never append `; echo $?` to a lane invocation. It makes the harness report exit
0 while a failure sits in the log; it has bitten this epic twice.

### Quiet rare-symptom campaign at wave close

W8 includes one long, serialized campaign on the dedicated machine after the
Wave D code freeze. Pin the exact integrated commit, leave the machine otherwise
idle (no CI, benchmark, sanitizer, or second test campaign), and repeat the
historical cancellation-answer instrument that exposed RV2-DEBT-261/263:

```bash
SURGE_STDLIB=$(pwd) SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
taskset -c 8-15 go test ./internal/vm \
  -run '^TestRuntimeV2(FailfastJoinAnswersCancelled|TimeoutTargetAnswersCancelledToEveryHandle)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 300s
```

Run at least 800 non-vacuous iterations; prefer 1000 while the measured cost
remains about seven seconds per iteration. A run is a failure when either named
row is absent or skipped, not only when `go test` exits nonzero. Record the
commit SHA, checkout, CPU affinity, exact command, wall time, pass/fail/vacuous
counts, and every distinct failure signature. Do not split this timing-sensitive
campaign 8+8: the historical evidence is load-dependent and was measured with
the command above on CPUs 8-15. The ordinary full closeout gates run before and
after the campaign; this loop is not a substitute for them.

## 7. Remaining work — the dispatch list, ordered, 2026-08-28

Established against `03379549`. Each item names its entry condition and the
production files it will touch, because two lanes at one file is how this wave
has produced its integration conflicts. **The file conflicts are real and are
stated: W3, W4 and W5 all reach `internal/backend/llvm/emit_async.go`, and W2
and W3 both reach `runtime/native/rt_task_complete.c`.** Those pairs are
sequenced, not parallel.

### W1 — record RV2-DEBT-080's owed run and close D6

Entry: none. The code is on the tree; only the measurement is missing.

Run, on the dedicated machine, and record the readings in `DEBT.md` and
`NOTES.md`: `make runtime-v2-carrier-sanitizer-check` (whose second recipe line
already names a-3's four valgrind rows and the cancelled-owned-result row),
`make runtime-v2-heap-check`, and `make runtime-v2-lifecycle-check` (which runs
`TestRuntimeV2LifecycleDebt080*`). Repeat the lifecycle line under the gate, not
the rows alone — 2026-08-27 established that a row green twenty times in
isolation says nothing about the same row under its gate.

Files: `docs/runtime-v2-epics/DEBT.md`, `docs/runtime-v2-epics/NOTES.md`. No
production file. Size: hours, dominated by gate wall time.

### W2 — the fourth cancellation-answer window, under the gate

Entry: W1's lifecycle-gate run, which is the instrument. A red
`TestRuntimeV2FailfastJoinAnswersCancelled/llvm/threads-4` under the gate is the
entry; the row run alone is not.

RV2-DEBT-261 and 263 are reopened, 265 has been measured out as not the window
(3 red of 200 before it, 1 of 200 after), and the residue is roughly half a
percent. Find the fourth window and pin it with a sync point plus a negative
control, as 263 was. RV2-DEBT-291 (`panic: async: task slot out of range`, a
task-table segment still `NULL` at an id whose creator was told it was there)
is a different defect surfaced by the same instrument and rides with this lane.

**Measurement update 2026-08-28:** the entry condition is not currently met.
The exact gate line completed 200 and then 600 non-vacuous iterations on the
dedicated machine with zero red at `cf20c74d`/`4db2f120`. W2 therefore remains
dormant until that instrument produces a red; absence of a red is evidence, not
a guessed fourth-window fix. W8 repeats the same instrument after the final
integrated freeze so later Wave D changes cannot silently restore the symptom.

Files: `runtime/native/rt_task_complete.c`, `rt_async_poll.c`,
`rt_async_task.c`, `rt_task_table.c`, `rt_scope.c`, `rt_sync_point.{h,c}`;
`internal/asyncrt/task_complete.go`; `internal/vm/vm_terminator.go`;
`internal/vm/runtime_v2_lifecycle_*_test.go`. Size: several days — three windows
have been found behind this one symptom and each took a lane.

### W3 — close P3

**The three deterministic rows are DONE, 2026-08-28** — shutdown versus a
claimed clone, cancel versus `READY`, and stale-generation late publication,
each with the negative-control build §7 of the epic requires, each wired into
`runtime-v2-lifecycle-check`, and the record written into §4.1 the way P2's was.
Touched: `runtime/native/rt_sync_point.{h,c}`, `rt_task_entitlement.c`,
`rt_task_complete.c`, `rt_task_lifetime.c`, `rt_task_result.c`,
`check_sync_points.sh`, `Makefile`,
`internal/vm/runtime_v2_task_entitlement_{syncpoint,stand}_test.go`,
`internal/vm/runtime_v2_lifecycle_behavior_{harness,await_shutdown}_test.go`.
It did not need `rt_async_task.c`: the window a claim is out in belongs to
`rt_task_entitlement_begin_take`, not to the await that calls it.

WHAT REMAINS OF W3, and it is not a sync-point row: RV2-DEBT-304 (the rollback
failpoint, blocked on the clone slot returning `void`) and RV2-DEBT-305 (the two
bounded child-process controls). 266 is an ABI decision and belongs with whoever
owns the `ValueOps` shape; 267 is stand work and can run beside anything, since
it touches no production file. Size: 266 unknown until the ABI call is made,
267 a day.

### W4 — D7's tail: the frame answers for what it holds

Entry: D4 (met). Must not run beside W5 — both rewrite
`internal/backend/llvm/emit_async.go` and `emit_async_helpers.go` — and not
beside W2/W3, which own `rt_task_complete.c`.

Implement §11's async-frame paragraph: one compiler-generated frame/state
descriptor table, each suspension state naming its concrete resume type and
slot, a state generation the producer must match before initializing, and a poll
that claims and empties the resume slot exactly once. That is also what retires
RV2-DEBT-179: the frame itself, not the calling convention, says whether it
still owns its contents, so the walking and shallow releases become one release
that can tell — proven by a negative row in which a frame abandoned through the
wrong path is refused at build time or trapped at run time, and by the existing
valgrind witnesses staying green. Target: `suspension-frame-owner` reads live
zero.

Files: `internal/backend/llvm/emit_async.go`, `emit_async_helpers.go`,
`emit_aggregate_ops.go`, `emit_calls.go`, `emit_drop_glue.go`;
`internal/mir/lower_expr_crossing_spawn_poll.go` and the
`AsyncStateFreeBuiltin` declaration; `runtime/native/rt_async_poll.c`,
`rt_task_complete.c`, `rt_async_internal.h`; `internal/vm/async_runtime.go`,
`vm_dispatch_async_types.go`. Size: several days — it is the only Wave D storage
owner that has not been touched at all, and it crosses both backends.

### W5 — D8's tail: delete the adopt leg

Entry: W4 integrated (file conflict on `emit_async.go` and
`emit_async_helpers.go`).

Give the select's winner index its own typed return so neither caller needs a
word bridge, then delete `emitI64ToValue` and the `inttoptr` in its body.
`emit_term.go`'s single `inttoptr` is a deliberate constant reinterpretation and
should be re-read rather than assumed to be part of this. Prove it the way the
row asks: the transport round-trip test family passes against the inline path
with no copy at the boundary, on both backends. Target: `llvm-erased-word-
bridge` and `llvm-pointer-word-ir` read live zero and RV2-DEBT-151 retires.

Files: `internal/backend/llvm/emit_async_helpers.go`, `emit_async.go`,
`emit_crossing_select.go`, `emit_term.go`; the select lowering in
`internal/mir`. Size: a day — three call sites and one helper, but it is the
row's retirement, so the round-trip family is the cost.

**DONE 2026-08-29, and three of the four sentences above need correcting.**

`emitI64ToValue` and both its `inttoptr` spellings are gone. The typed return
lives in a new `internal/backend/llvm/emit_select_winner.go` and is shared by
both callers, which is where it HAD to go: only the local caller reads
`rt_select_poll`, the crossing caller loads a 64-bit wire field out of the
anchored reply, so narrowing the C entry point would have left the second caller
unchanged. Neither `emit_term.go` nor the `internal/mir` select lowering was
touched — the lowering already gave the destination the winner-index type, and
what was missing was the emitter checking it. `llvm-erased-word-bridge` 3 -> 0,
`llvm-pointer-word-ir` 3 -> 1.

The one that remains is `emit_term.go:291`, re-read as asked, and it stays: it
is an LLVM CONSTANT EXPRESSION, `inttoptr (i64 N to ptr)`, building the tagged
immediate `rt_bignum_tag.h` defines and `fixi_box` builds. It reinterprets a
compile-time constant, not a runtime carrier. So "read live zero" is unreachable
for that category and always was; what it reaches is zero unallowed findings and
zero migration carriers, with one base-census legacy finding under the reviewed
permanent allowance `fixnum-inline-tagged-word`.

RV2-DEBT-151 does NOT retire with this. Its narrowing of 2026-08-28 moved the
open half to the VM (`internal/vm/transport_storage.go`, three operations,
twelve call sites), which no part of this step touches.

**The scanner's token list is not part of the deletion.** Removing
`case "emitValueToI64", "emitI64ToValue"` from `internal/carriergate/scan_go.go`
was tried and measured: `TestLegacyCarrierManifestMatchesExactBaseCensus` fails
with 25 `stale legacy` lines and `TestScanIsLexicalCommentSafeAndDeterministic`
fails on its own fixture. The frozen base census is re-derived by scanning
`7df10725` with the CURRENT scanner, so pruning a token falsifies the census of
a commit that contained 25 of them. Every category already at live zero
(`vm-boxed-composite-kind`, `llvm-composite-to-ptr`) keeps its tokens for the
same reason.

### W6 — D2's measurement close

Entry: an owner decision on RV2-DEBT-174. The carrier bench cannot run against
its digest-frozen `epic_base` compiler at all since the `Channel::<T>::new`
migration, because `scored/maps-scalar` and `scored/maps-composite` are written
with `Map::<K, V>.new()`, which `7df10725` does not know. The owner ruled on
2026-08-26 that the base is the latest green commit with no pin; re-capturing
the frozen manifest is the act that implements it, and it is not a lane's.

Then re-measure with ≥3 agreeing runs and close RV2-DEBT-156's bench clause and
RV2-DEBT-157's parity row for a heap-owning value. Two budgets are already known
stale in both directions: `blocking-composite` 277 → 341, `channel-buffered-
composite` 78 → 14.

Files: `testdata/runtime-v2-carrier-bench.json`, `scripts/runtime_v2_carrier_
bench*.py`, `docs/runtime-v2-epics/DEBT.md`. Size: a day after the ruling; it is
blocked, not sized, before it.

### W7 — D3b: the channel's own lifetime

Entry: none for C1; C3 follows C1. Not in the D1–D8 list because D3 closed
before the channel's lifetime was a question, but it is this wave's and it is
open.

C0 landed (`8c9851a6`, `1a0b5914`, `743f034e`): `handle_refs`, `pins` and
`reclaiming` in `runtime/native/rt_channel_refcount.{h,c}`, with the far
registry releasing through `rt_channel_handle_drop`. **Nothing else calls it**:
`rt_channel_handle_retain`/`_drop` are declared in the emitter's builtin table
and emitted by no site, so no program's `Channel<T>` copy retains and no drop
releases (C1), and no pin is taken by a waiter, a select subscription or a
claimed slot (C3). RV2-DEBT-259 rides here: `rt_channel_free` does not perform
the teardown order §7 prescribes — no dying mark under the owner lock, no
detach-then-invalidate pass — and it was recorded rather than patched in a lane.

Files: `internal/backend/llvm/emit_channel.go`, `emit_channel_storage.go`,
`emit_drop_glue.go`, `builtins.go`; `runtime/native/rt_channel_refcount.c`,
`rt_async_channel.c`, `rt_channel_lane.h`, `rt_far_channel.c`. Size: several
days for C1+C3 together; RV2-DEBT-155 closes with them.

### W8 — wave closeout

Entry: W1–W5 and W7 integrated on one tree. W6 gates only RV2-DEBT-156/157.

The §12 command list in full on the integrated tree, plus the four additions in
§6 above, plus a re-run of the live carrier scan: the number that says Wave D is
done is `suspension-frame-owner` and `llvm-erased-word-bridge` at live zero and
`llvm-pointer-word-ir` at live ONE, with what remains confined to
`rt_remote_task_*` and `rt_far_channel*` — Wave E's, by name and by file. The
one is `emit_term.go`'s allowed fixnum constant; W5 records why raw zero there
would mean the fixnum representation had changed, which is not this wave's.

After those deterministic gates are green and the integrated SHA is frozen,
run §6's quiet rare-symptom campaign. Wave D does not close on fewer than 800
non-vacuous iterations, on a campaign sharing the machine with another job, or
on a report that omits the exact SHA and failure-signature census.

### Not Wave D, recorded so no lane picks it up by mistake

`native-payload-bits` (19), `native-word-carrier` (10) and the far half of
`numeric-drop-dispatch` (13) live entirely in `rt_remote_task_*` and
`rt_far_channel*` and belong to Wave E. `vm-universal-owner` (7) is the VM's
`Value` frame slot and is not a Wave D owner. `composite-box-marker` (8) is four
glue-function names plus `cloneValueComposite` (RV2-DEBT-246).
`vm-async-any-carrier` (2) is `heap.Interface` in `internal/asyncrt/timer.go`.
RV2-DEBT-180 (the runtime's virtual clock, which ruling Р2 turns into a removal)
is blocker-class but is the multi-carrier work's, not this wave's.
