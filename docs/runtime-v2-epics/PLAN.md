# Runtime V2 — state, exit conditions, and remaining order

The migration's technical plan. It states what is left, what closes each
remaining wave as a number, and in what order the work is taken. It carries no
session history and no near-term dispatch: **what is in flight right now lives
in `NOTES.md`**, and the current wave's task list lives in
`23b-wave-d-execution-plan.md` §7.

Every figure is measured, with the command and the commit beside it. A figure
without one does not belong in this file.

Companions: `README.md` (epic history), `DEBT.md` (ledger), `RULES.md` (the
development rules every lane obeys).

---

## 1. The one number

The migration replaces erased carriers — values moved as a machine word plus a
type id — with typed ones. The carrier scanner counts what is left.

```
go test ./internal/carriergate -run '^TestLiveCarrierRatchetAgainstRepository$'
```

**83 live findings against a frozen base census of 626 — 87% of the v1 carrier
surface is retired**, measured 2026-08-28 at `03379549` and unchanged at
`20755cf6`. The ratchet is green: nothing live sits outside the manifest.

The residue is not spread thin. It is concentrated in four places:

| category | base | live | owner |
| --- | ---: | ---: | --- |
| `suspension-frame-owner` | 7 (+12 migration) | **18** | Wave D / W4 — the async frame |
| `native-payload-bits` | 134 | 19 | Wave E — `rt_remote_task_*` only |
| `numeric-drop-dispatch` | 188 | 13 | Wave E — `rt_far_channel*` + crossing emitters |
| `native-word-carrier` | 85 | 10 | Wave E — `rt_far_channel*`, `rt_remote_task_*` |
| `composite-box-marker` | 54 | 8 | all eight are `cloneValueComposite`; a rename |
| `vm-universal-owner` | 13 | 7 | NOT a Wave D owner — the VM's `Value` frame slot |
| `llvm-erased-word-bridge` | 25 | 3 | Wave D / W5 — the select adopt leg |
| `llvm-pointer-word-ir` | 3 | 3 | the same helper's body plus `emit_term.go` |
| `vm-async-any-carrier` | 23 | 2 | `internal/asyncrt/timer.go`, `heap.Interface` |
| `vm-boxed-composite-kind` | 74 | **0** | done |
| `llvm-composite-to-ptr` | 5 | **0** | done |
| `untyped-capture-state` | 15 | **0** | done |

Read plainly: **Wave D owns 32 of the 83. Wave E owns 42. Nine belong to
neither** and are named in §5 so no lane picks them up by mistake.

---

## 2. Where the epics actually are

Epics 1 through 25 are closed except three. That is the whole picture, and it is
smaller than the epic count suggests.

| Epic | State, measured |
| --- | --- |
| 1–20, 23, 24, 25 | **Closed.** History in `README.md`. |
| 21 — owner-routed frees | **Core shipped; acceptance closeout open.** Only the bench/matrix/seam/debt closeout is unperformed. |
| 22 — numeric reclamation | **Parked on purpose.** `float` shipped; `int`/`uint` reclamation resumes AFTER 23b, because one typed carrier model makes it simpler than supporting both. |
| 23b — inline storage and typed carriers | **In flight. This is the whole remaining migration.** |

So the migration is: **finish 23b, then Epic 22 Phase 2, then Epic 21's
closeout.** Nothing else is open.

### 23b's own waves

| Wave | State | Evidence |
| --- | --- | --- |
| A — baseline and census | Closed | the ratchet and its frozen base exist and are green |
| B — layout/operations foundation | **Closed in substance; the document says otherwise and the document is stale.** | The 2026-08-20 status correction says "items 3 and 5 say GENERATE and nothing generates". Measured 2026-08-28: `internal/backend/llvm/emit_value_ops.go` emits a real `move_init.type<N>` body (`:166-174`), the `plan_cross` slot is emitted, and `emit_task_result.go:28-45` refuses a module whose type has no descriptor. Owner migrations are NOT standing on an unmet entry condition. **Action: correct that paragraph.** |
| C — ordinary storage | Closed | both backend workstreams landed |
| D — containers and local carriers | **In flight.** D1, D3, D4, D5, D6 closed; D2 code-complete but unmeasured; D7 untouched until today; D8 waits on D7 | §4.1 of the wave-D plan |
| E — far carriers, leases, byte credits | Not started. Owns 42 of the 83 live carriers. | |
| F — diagnostics, deletion, closeout | Not started. Deletes the legacy symbols once live reads zero. | |

---

## 3. What "done" means — three numbers, not prose

A wave closes when a number says so. These are the numbers.

**Wave D is done when** `suspension-frame-owner`, `llvm-erased-word-bridge` and
`llvm-pointer-word-ir` all read **live zero**, and what remains is confined to
`rt_remote_task_*` and `rt_far_channel*` — Wave E's, by name and by file.

**Wave E is done when** `native-payload-bits`, `native-word-carrier` and
`numeric-drop-dispatch` read **live zero**, and saturation is proven to park a
producer without a busy retry, with every byte credit returning after adopt and
after drop.

**Wave F is done when** the live census reads **0 of 626**, the legacy symbols
are deleted rather than merely unused, and the twenty-gate aggregate
`make runtime-v2-check` reports twenty passes on one tree.

**23b is done when** all three hold at once on one commit, on both backends.

---

## 4. The order, with sizes

Sizes are in LANE-DAYS — one focused worker, one day — not wall clock. Lanes run
in parallel where files allow, and the conflicts are stated because two lanes at
one file is how this wave produced every integration conflict it has had.

### Now — Wave D's tail

| # | Work | Lane-days | Conflicts with |
| --- | --- | ---: | --- |
| W4 | **D7: the frame answers for what it holds.** Designed 2026-08-28 by a judged panel; the winning shape is a lifecycle word in the frame plus `task->frame_ops`, no new ABI record, no descriptor table. 14 ordered steps. | 4 | W5, W2/W3 (`rt_task_complete.c`) |
| W7 | **D3b: the channel's own lifetime.** C1 landed 2026-08-26. What is live: the pins have ZERO callers so the fail-closed reclaim gate is vacuous, and `rt_channel_free` performs none of §7's teardown order. | 3 | none (disjoint from W4 by function) |
| W2 | **The fourth cancellation-answer window.** Residue is roughly half a percent under the lifecycle gate. Three windows have been found behind this one symptom and each took a lane. | 3 | W3, W4 |
| W6 | **D2's measurement close.** Unblocked: the owner ruled 2026-08-26 that the bench base is the latest green commit with no pin. Re-capturing the frozen manifest is the act that implements it. | 1 | none |
| W5 | **D8: delete the adopt leg.** Give the select winner a typed return, delete `emitI64ToValue`. | 1 | W4 (must follow it) |
| W3 | **P3's tail.** The three deterministic rows landed 2026-08-28. The rollback failpoint is **not owed** — the owner ruled a local duplication that cannot finish is fatal. What replaces it is bigger and belongs to W4's files: every generated move/copy/clone body owes an allocation test, because today the failure is a segmentation fault rather than a fatal error. Two bounded child-process controls remain. | 2 | W2, W4 |
| W8 | **Wave closeout.** The full command list on the integrated tree plus a live carrier scan. | 1 | everything |

**Wave D tail: ~15 lane-days, ~2 calendar weeks with 3 lanes in flight**, given
that W5 must follow W4 and W2/W3/W4 cannot share `rt_task_complete.c`.

### Next — Wave E

Six numbered items in `23b-inline-storage-and-typed-carriers.md` §5, all landing
in `rt_remote_task_*` and `rt_far_channel*`. The transport half already has its
owner rulings from 2026-08-28: pointer transport gets no byte credits, a data
slot plus a reserved control slot, and `payload_len` means transport-owned bytes
or is removed.

**Estimate: ~12 lane-days, ~2 calendar weeks.** Lower confidence than Wave D —
the wave has had no reconnaissance pass yet, and its saturation proof (item 6)
is the kind of liveness work that has historically cost more than its estimate.

### Then — Wave F

Diagnostics (one tri-state clonability classifier, type-directed advice, the
Help channel and LSP mapping), then deletion, then the closeout gates.

**Estimate: ~8 lane-days, ~1.5 calendar weeks.** The diagnostic half is
well-specified and low-risk; the deletion half is mechanical once live reads
zero; the closeout is gate wall-time.

### After 23b

| Work | Lane-days |
| --- | ---: |
| Epic 22 Phase 2 — `int`/`uint` reclamation | ~5 |
| Epic 21 closeout — bench/matrix/seam/debt | ~2 |

### The whole thing

**~42 lane-days. At three lanes in flight, roughly six calendar weeks to a
migration that is done rather than mostly done.** That assumes no new blocker of
the kind that parked Epics 22, 23 and 24 in turn — and the honest history is
that this migration has hit one such blocker per epic. Add a 30% blocker
allowance and it is eight weeks.

---

## 5. What we do NOT pick up

**A defect found during a wave goes into `DEBT.md` and is NOT worked** unless it
blocks that wave's exit condition — the numbers in §3 (owner rule, 2026-08-27).

Specifically parked, so no lane picks them up:

- **`vm-universal-owner` (7 live)** — the VM's `Value` frame slot. Not a Wave D
  owner, not a Wave E owner. It is a VM representation change and it has no
  epic. It does not block the census reading zero for the categories §3 names.
- **`vm-async-any-carrier` (2 live)** — `heap.Interface` in
  `internal/asyncrt/timer.go`. Unrelated to any carrier the model describes.
- **`TestLLVMParity/random_pcg32` and `/hash_xxh64`** — red since at least
  2026-08-26, bisected. A VM-lane value-model defect. Recorded as RV2-DEBT-306.
- **The sixteen stdlib modules that do not reach MIR under the corpus's
  no-entrypoint profile** — RV2-DEBT-302, correctly diagnosed as a
  monomorphization use-site loss and explicitly not a stdlib source defect.
- **RV2-DEBT-307** — a borrow captured by an ordinary `spawn` crosses a carrier
  boundary with no diagnostic. This is a real soundness hole and it is a
  LANGUAGE change, not a runtime one; its scheduler half needs an owner ruling
  on whether carrier pinning is transitive before it can be implemented.
- **RV2-DEBT-180** — the runtime's virtual clock. Blocker-class, but it belongs
  to the multi-carrier work, not to 23b.

## 6. What the owner ruled, 2026-08-28

Four questions blocked work. All four are answered, and each is recorded where
the paragraph it interprets lives, not only here.

| Question | Ruling | Where it is written |
| --- | --- | --- |
| Does the `ValueOps` clone slot return a status so a half-built duplication can roll back? | **No — a local duplication that cannot finish is FATAL.** A crossing returns a status because it has a caller that can still answer; a local clone does not, and recoverable out-of-memory is outside this epic. **But the tree does not detect the failure at all today**: the generated body calls `rt_alloc` and tests nothing, while `rt_alloc` answers `NULL` without panicking, so the present behaviour is a segmentation fault rather than a fatal error. Every generated move/copy/clone body owes an allocation test that panics naming the type. | `23-storage-model…md` §3 |
| Is carrier affinity transitive, and what does `blocking { }` do with a live borrow? | **Affinity is a function of the CAPTURE SET, not of the parent-child edge.** Not inherited down the tree; transitive through borrowing. A `blocking` body may not capture a borrow and is refused as a crossing is. The feared deadlock does not exist: submitting a blocking body PARKS the task rather than occupying its carrier. | `RUNTIME_V2.md` §9 |
| Do the legacy `cloneValueComposite` rows retire with RV2-DEBT-246? | **Rename.** Re-counted: all EIGHT live findings are that one symbol, three of them in comments, and `AllocStruct`/`AllocTag` no longer exist. The duplication is already correct — what is counted is a NAME from the boxed-composite era. Precedent: `Payload` → `TaskState`. | `DEBT.md` RV2-DEBT-246 |
| Is `QUEUE_FULL` visible once saturation parks? | **No — the sender PARKS on its own shard.** The status stays internal so the parking code can be told there is no room; no crossing answers a program with it and no language surface gains a failure arm. Two obligations travel with it: an admission stall is a MEASURED NUMBER, and a park that cannot be released stays reachable by cancellation. | `RUNTIME_V2.md`, transport open questions |

Nothing in §4's schedule waits on an owner answer. The questions that remain are
in `DEBT.md`, attached to work that is deliberately not scheduled.
