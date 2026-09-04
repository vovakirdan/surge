# Runtime V2 — the board

What is being worked right now, what is taken next, and in what order. Updated
IN PLACE, not appended to: this file is a board, not a log.

It does not restate the migration. `README.md` holds the epic history and
roadmap, `DEBT.md` the ledger, `23b-*.md` the wave definitions, `RULES.md` the
rules every lane obeys. Read those to know why; read this to know what.

**State line, 2026-08-30 at `45c54b22`.** Live carrier census **60 against a
frozen base of 626** (the number as written that day; the manifest
`legacy_carriers.json` froze **683** on `7df10725`, and 683 is the base every
later census reads against -- corrected 2026-09-03, Wave F F3), ratchet green. Epics 1–25 are closed except three: 21
(closeout only), 22 (parked until 23b closes), 23b (in flight — the whole
remaining migration).

**`runtime-v2-carrier-check` was RED on the trunk from `9d710bcb` to
`4968061f`, and no gate said so.** The sync-point rename edited the manifest
and left five rules in the loader and the validator demanding the shape it had
replaced, so `load_manifest` raised inside `setUpClass`: the canonical class
never ran, the suite reported 53 tests instead of 59, and the nineteenth
sub-gate of the twenty on the aggregate roster was red while the board recorded
the aggregate as the thing being counted. A suite that cannot start reads
exactly like a suite that passed. Closed by `4968061f`, which also adds the row
that asserts the probe shape from the loaded object.

**The aggregate as a COUNT at `45c54b22`: 3 green, 2 red of 5** on the dedicated
machine (`/root/w8.sh 5` over the pinned clone `/root/surge-gates`), 656–808
seconds each, with no CI job on the machine while it ran. The two reds are
**different gates, and neither is the row the previous count named**:

| run | seconds | verdict | gate, and the row inside it |
| ---: | ---: | --- | --- |
| 1 | 808 | RED | `runtime-v2-heap-check` — `TestRuntimeV2ChannelHandleValgrindZero/outlives_scope`, 182.01s against a 180s budget, stderr holding nothing but the Memcheck banner, siblings 2.96–3.03s |
| 2 | 656 | green | |
| 3 | 695 | green | |
| 4 | 657 | green | |
| 5 | 672 | RED | `runtime-v2-http-owner-check` — `TestRuntimeV2HTTPOwnerLocalBehavior/shards-8`, `client 1 read response: read tcp 127.0.0.1:16052->127.0.0.1:16625: i/o timeout`; `shards-1` and `shards-2` passed in the same run |

An earlier count at `b303a213` read 4 green of 5, and its red was a THIRD gate:
`runtime-v2-waiter-check` / `TestRuntimeV2NetWaiterTraceContract`, closed since
as a stand defect (RV2-DEBT-310). So across two counts the aggregate has been
red at four distinct gates, each once or twice, none of them the same one twice.
**W8 is not one row away from green.** Every one of the four is
topology-sensitive — an eight-shard arm, a load-sensitive trace race, a valgrind
row whose thread count is `sysconf(_SC_NPROCESSORS_ONLN)` because these rows do
not pin `SURGE_THREADS` the way `envForParity` does — which says the class to
close is the unpinned topology, not the four rows one at a time.

**The twenty-gate aggregate passed ONCE, and once is not a measurement.** It
exited 0 on the dedicated machine at 675 seconds, twenty of twenty. A later run
of the same roster on the same machine failed at `runtime-v2-transport-check`,
and the row responsible turned out to fail **81 times in 100** there while
passing 3 of 3 here. A row that fails four times in five passes one run in five,
so the green run was a sample. The defect predates this wave -- bisected on the
machine that sees it, red at every commit back to 2026-08-26 -- and it is
RV2-DEBT-308. **W8's closeout must state the aggregate's greenness as a count of
runs on the machine that fails, not as one exit code.**

No scheduled work waits on an owner decision.

---

## In flight

**Nothing. Waves D, E and F are closed** (2026-09-04), and with them the
whole of Epic 23b. The board keeps the shape of what closed them so a reader
can tell a finished wave from an abandoned one.

| Wave | Closed by | The evidence that closed it |
| --- | --- | --- |
| **D** | `625926c4` | The freeze set on `43ae205a` with the judge on `2b849208`: the 1000-repeat fail-fast campaign green after the judge was rewritten to assert the PROPERTY ("a block answers Cancelled exactly when a member did") instead of a schedule, W8 twice, the rate stand at 200 of 200, RV2-DEBT-312 closed by measurement. |
| **E** | the far-carrier and transport work | The three census categories the wave owned -- `native-payload-bits`, `native-word-carrier`, `numeric-drop-dispatch` -- read live zero, and saturation parks a producer without busy retry (`anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it`, parks=1 wakes=1, with its Rule-13 control). |
| **F** | this closeout | The census reads 0 in every category a wave owned; the roster is green as a COUNT on the dedicated machine; the Epic 21 Task 9 matrix runs at 1, 2 and 8 shards under memcheck; the paired benchmark passes under the protocol the owner ruled on 2026-09-04; five review lenses, Sentrux, the sanitizer gate and golden twice. |

**What the waves left open, and why none of it blocks a closeout.** Each is a
row in `DEBT.md` with its own owner; none is a wave's exit condition.

| | What stays open |
| --- | --- |
| RV2-DEBT-061 | A rare invalid free under valgrind on the immediate-`on` retry path, 10-25 % of runs. Pre-existing, found 2026-07-20, not a crossing-representation defect. |
| RV2-DEBT-318 | The VM's 27 migration paths, counted and pinned by the live ratchet. The owner ruled 2026-09-03 that removing them is a VM-representation epic of its own, not this wave's. |
| RV2-DEBT-323, 325-328 | The shutdown family and its neighbours, opened by wave work and scheduled after it. |
| RV2-DEBT-333 | The release build compiles the runtime and the program's IR with no `-O`. Both sides of every paired benchmark are built that way, so the ratios compare like with like; the optimisation level is an owner decision with its own re-measurement. |

## Next, in this order

1. **Epic 22 Phase 2** — `int`/`uint` reclamation, the successor the detour
   chain was taken for. **Scope settled 2026-09-04, variant (2): the crossing
   barriers come FIRST, for all three arbitrary-precision types, and `int`/`uint`
   then joins a finished mechanism.** The barriers were assigned to Epic 23b and
   never built, so this is 23b's undelivered half arriving under Epic 22's name;
   what 23b DID build is the typed-carrier plumbing they hang on. A second
   ruling the same day fixed what the capability verdicts mean — `CrossClonable`
   is "possible via deep clone", not "raw bits copy", and flag, descriptor,
   registry hash and `Dump` must derive from one backed state rather than a late
   mask. Both are in `22-numeric-reclamation.md`, "Phase 2's Scope Question".
2. **The VM representation epic** — `vm-universal-owner` and
   `vm-async-any-carrier`, the 55 live carriers the 2026-09-03 ruling pinned
   and sent here rather than to Wave F.
3. **RV2-DEBT-333's ruling** — the optimisation level for the runtime and the
   emitted IR, with a paired benchmark on both sides of whatever is chosen.

---

## File claims — who may touch what

Two lanes at one file is how this wave produced every integration conflict it
has had.

| Claimed by | Files |
| --- | --- |
| W4 | `internal/backend/llvm/emit_async*.go`, `internal/mir/async_codegen.go`, `internal/mir/suspension_frame.go`, `runtime/native/rt_task_complete.c`, `rt_async_poll.c` |
| W7 | `runtime/native/rt_channel*`, `rt_async_channel.c`, `rt_far_channel.c`, `internal/backend/llvm/emit_channel*.go` |
| Scope refusal | `internal/sema/spawn_scope_adoption*.go`, `internal/diag/codes.go`, `runtime/native/rt_async_task.c` |
| free | everything else, including the rest of `internal/sema` |

`internal/backend/llvm/emit_drop_glue.go` is shared by W4 and W7 but not in the
same function: W4 owns the suspension-frame family, W7 owns `typeOwnsHeapRec`,
`emitDropHandle` and `fieldDropIsExclusive`.

This table divides files, not the tree. Concurrent lanes each work from their own
worktree and integrate through commits — Global Rule 17, owner's ruling of
2026-08-30, after three lanes in one checkout cost a duplicated instrumentation,
a rate stand that measured a neighbour's edit, and a timing run exposed to a
neighbour's load.

---

## After the waves

Taken in order, not in parallel with the above.

| | What | Size | Closes when |
| --- | --- | ---: | --- |
| **Wave E** | ~~Far carriers, leases, byte credits.~~ **CLOSED 2026-09-04.** Byte credits are not part of it and never will be: the 2026-08-29 ruling found pointer transport charges no per-message bytes, and the budget that exists is slots. | — | `native-payload-bits`, `native-word-carrier` and `numeric-drop-dispatch` read live zero, and saturation parks a producer without busy retry. **Met.** |
| **Wave F** | ~~Diagnostics, deletion of the legacy symbols, the closeout gates.~~ **CLOSED 2026-09-04.** | — | The census reads **0 in every category a wave owned** (owner ruling 2026-09-03, variant (а): of the manifest's frozen 683 on `7df10725`, the 55 that remain live are the VM's `Value` interchange type and the async runtime's `any`, counted and pinned by `TestLiveCarrierRatchetAgainstRepository`, and their removal is a VM-representation epic of its own), the symbols are deleted rather than unused, and `make runtime-v2-check` reports every sub-gate on its roster passing on one tree. **Met.** |
| **Epic 22 ph. 2** | Crossing barriers for all three types, then `int`/`uint` reclamation (owner ruling 2026-09-04, variant 2). | — | — |
| **Epic 21** | ~~Bench, matrix, seam, debt closeout.~~ **CLOSED 2026-09-04** with Wave F: RV2-DEBT-125. | — | — |

Sizes are lane-days: one focused worker, one day. What remains of this
migration is Epic 22 Phase 2 and the VM representation epic; the ~39 lane-days
this table used to project were spent, and the blocker allowance it asked for
was used twice -- once on the fail-fast judge's premise, once on the paired
benchmark's protocol.

---

## Not picked up

A defect found during a wave goes to `DEBT.md` and is NOT worked unless it
blocks a wave's exit condition (owner rule, 2026-08-27). Named so no lane takes
them by mistake:

| | Why not |
| --- | --- |
| `vm-universal-owner`, 7 live when written; 40 live plus 27 RV2-DEBT-318 migration paths on 2026-09-03, after the 2026-09-01 structural walk (NOTES F6) | The VM's `Value` frame slot. Neither wave's owner; a VM representation change with no epic. |
| `vm-async-any-carrier`, 2 live when written; 14 on 2026-09-03 (the structural walk counts `Task.State`, `Channel.buf` and their kin as well) | `heap.Interface` in `internal/asyncrt/timer.go`. Not a carrier the model describes. |
| RV2-DEBT-306 | `TestLLVMParity/random_pcg32` and `/hash_xxh64`, red since 2026-08-26, bisected. VM-lane value model. |
| RV2-DEBT-302 | Sixteen stdlib modules do not reach MIR under the corpus profile. A monomorphization use-site loss, explicitly not a stdlib source defect. |
| RV2-DEBT-307 | A borrow captured by an ordinary `spawn` crosses a carrier boundary with no diagnostic. Real, and a LANGUAGE change rather than a runtime one. Unblocked by the affinity ruling but outside 23b. |
| RV2-DEBT-180 | The runtime's virtual clock. Blocker-class, but the multi-carrier work's. |

---

## Owner rulings a lane must not re-open

Settled 2026-08-28. Each is written in full where the paragraph it interprets
lives; this is the index.

- **A local duplication that cannot finish is FATAL.** The clone slot keeps its
  `void` return and the rollback failpoint is not owed. What IS owed is the
  allocation test — item 4 above. `23-storage-model…md` §3.
- **Carrier affinity is a function of the capture set**, not the parent-child
  edge: not inherited down the tree, transitive through borrowing. A `blocking`
  body may not capture a borrow. `RUNTIME_V2.md` §9.
- **A sender with no transport slot parks**, and `QUEUE_FULL` stops being an
  answer a program observes. An admission stall must be a measured number, and
  the park must stay reachable by cancellation. `RUNTIME_V2.md`, transport.
- **`composite-box-marker` retires by RENAME.** All eight live findings are
  `cloneValueComposite`; the representation was already established correct.
  `DEBT.md` RV2-DEBT-246.
- The frame descriptor **table is the compiled per-arm dispatch**, the frame
  gains **no generation**, a wrong-path abandonment is refused at build time
  **and** trapped at run time, and the descriptor is reached **by pointer**.
  `23-storage-model…md` §11.
