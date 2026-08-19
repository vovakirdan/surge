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
| 7 | `copy_init` — `valueops/flags.go:80` marks it `structural: true` and `entry.go:145-147` says the runtime's generic byte copy satisfies it, while `rt_slot_control.c:44` demands `COPY ⟺ copy_init != NULL` and **no such symbol exists** | **Add the generic byte-copy symbol to the runtime** and bind it. The C-side check stays strict |
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
`rt_slot_control.c:44` keeps demanding `copy_init != NULL`; `valueops`'
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
| D4 | Task canonical result/resume and cloned result entitlements | D0; may run beside D3 only once their production files do not overlap |
| D5 | Local `select` staging and losing-arm cleanup | D3 |
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
