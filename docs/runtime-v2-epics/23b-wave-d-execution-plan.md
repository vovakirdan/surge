# Epic 23b — Wave D Execution Plan

Containers and local carriers. Written 2026-08-11 against HEAD `f2641713`,
after a ruling-by-ruling pass over the eight standing blockers.

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

| Step | Owner | Entry condition |
| --- | --- | --- |
| D1 | Fixed arrays and dynamic array element buffers | D0 |
| D2 | Map key/value entries — insert, rehash, replace, remove, failed insert, teardown | D1 (element storage first) |
| D3 | Buffered/unbuffered channel send/receive and waiter mailboxes | D0.7 (the waiter fix is a precondition, not a parallel task) |
| D4 | Task canonical result/resume and cloned result entitlements. **D4a (typed result slot, near and far) CLOSED 2026-08-25**; D4b (entitlements) open | D0; may run beside D3 only once their production files do not overlap |
| D5 | REMOTE select only — `rt_far_channel_select.c`. Local `select` moved into D3 by the 2026-08-19 ruling below. **CLOSED 2026-08-25** | D3 |
| D6 | Blocking captures/results and every cancellation timing | D3, D4 |
| D7 | Async frames, captures, polling, wake, normal/shutdown drains | D4 |
| D8 | RV2-DEBT-151 retirement — local **and** FAR **and** CROSSING (ruling 8) | D1–D7 |

Worktree rule, from §5: task/channel/select/blocking may be separate worktrees
**only after** the shared owner/slot API is integrated and their production
files do not overlap. Subagent worktrees have twice come up on the wrong
lineage — the base commit is a required field in any worktree handoff.

### D2 carries a known live defect

DEBT-158's premise that the VM is safe by construction is FALSE. VM map
elements resolve POSITIONALLY (`intrinsic_map.go:246` returns
`Location{LKMapElem, Handle, Index}`; `place_container.go:221-224` resolves by
index), and VM remove IS swap-with-last (`intrinsic_map.go:421-431`). So
`let p = m.get_mut(&"b"); m.remove(&"b"); *p = 99` writes into the slot `"c"`
was swapped into and drops `c`'s live value. The VM is immune to ADDRESS
invalidation, not INDEX invalidation. D2 owns this; it is not native-only.

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

## 6. Closeout evidence

The §12 command list in full, on the integrated tree, plus:

- `make behaviour-check-all` and `make behaviour-check-mt`, both lanes, at the
  D0 exit and again at wave close — not the VM lane alone;
- `make runtime-v2-carrier-sanitizer-check` (which exists as of D0.5);
- every sync point recorded as a positive AND a negative control;
- `make runtime-v2-file-size-check EPIC_BASE=7df10725e001ddf915d536aa58f880bd7e04aafd`.

Never append `; echo $?` to a lane invocation. It makes the harness report exit
0 while a failure sits in the log; it has bitten this epic twice.
