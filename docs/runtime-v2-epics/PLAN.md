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

**Wave D — the only thing that closes the wave.**

| | State |
| --- | --- |
| Carrier categories | **MET.** `suspension-frame-owner` 0, `llvm-erased-word-bridge` 0, `llvm-pointer-word-ir` 1 allowed. Census 83 -> 60. |
| W6 — D2 closed by measurement | **MET.** Valgrind zero on the five map-teardown shapes, 10 of 10 on the dedicated machine, is the proof; the eight stale budgets are re-captured and re-pinned. See item 3 below for the premise that did not survive. |
| W8 — aggregate as a COUNT | **3 green, 2 red of 5** at `45c54b22`, and the two reds are different gates (`runtime-v2-heap-check`, `runtime-v2-http-owner-check`). See the state line: four distinct gates over two counts, all topology-sensitive. Counted again once the class is closed, not once a row is. |

**Contract debt from the 2026-08-29 rulings — PARKED, none of it closes the
wave.** Four lanes ran; their branches hold the work and their reviews hold the
reasons. Nothing here is abandoned and nothing here is progress on the epic.

| Branch | Verdict | What must happen before it lands |
| --- | --- | --- |
| `w-scope` | LAND_WITH_NOTES | The `creation_scope_key` refusal. Closest to landing of the four. |
| `w-result` | **DO_NOT_LAND** | Two blockers: `runtime-v2-net-handle-check` is RED on its tree and that gate is on the aggregate roster, and `internal/buildpipeline/build.go` crossed 500 effective LOC. It changes a runtime C ABI signature (`rt_tag_alloc` gains a parameter) and every result constructor's NULL contract -- a protocol change that did not run the full suite, which is what Rule 16 exists for. |
| `w-bench` | **DO_NOT_LAND** | A false fact in a committed comment, in a digest-pinned harness file. Rule 14's deliverable -- correcting the ledger row whose premise it disproved -- was not done. And it deferred the `final` phase on its own authority: the ruling renames the sync point and retires the byte assertions, and says nothing about deferral. |
| `w-farcancel` | **DO_NOT_LAND** | The row is VACUOUS: it observed `withdrawn_before_first_poll = 0` in 7 of 7 runs, entering the body 1024 times of 1024 -- it never takes the path it is about. It also claims a transport credit the header does not have, and one of its comments contradicts its own sibling row. |

## Next, in this order

1. **The two SOUND_WITH_GAPS lanes get their gaps closed and land** — the frame
   leak (`lane-d7-1`) and the channel pins (`lane-d7-3`). Both have real,
   measured findings; neither is integrable while its critic's list stands.
2. **The two UNSOUND lanes are re-sent, not repaired.** `lane-d7-2`'s field is
   right and its writers are not; `lane-scope-refusal` asks about the spelling
   of a binding where it must ask about the task. Each re-sent lane carries its
   critic's findings as part of the brief.
3. ~~**W6 — D2's measurement close.**~~ — **DONE 2026-08-30 by measurement.**
   Eight budgets were re-captured in ONE run of
   `make runtime-v2-carrier-baseline-capture`, which reports every mismatch
   instead of stopping at the first, and each read one number across all
   eighteen samples the protocol takes.

   **The premise this item used to carry was false and is corrected here rather
   than deleted.** It said `channel-unbuffered-composite` and `-scalar` "read 4
   in warmup and 3 in every measured pair, repeatably", and the 2026-08-29
   two-phase ruling was taken on that reading. The instrument says otherwise:
   both rows answer **4 in warmup and 4 in every measured pair**, eighteen of
   eighteen, on two separate capture runs a day apart. There is no phase split
   in any row. The wobble that was seen is the runtime TOPOLOGY going unpinned —
   one binary reads 343 then 350 for the same commit with no
   `SURGE_SHARDS`/`SURGE_THREADS`/`SURGE_BLOCKING_THREADS`, and 341 three times
   running with them. That is `RUNTIME_V2.md`'s own rule about a difference of
   two summed lane counters, and it is the ruling's SECOND half — per-lane
   counters and a named quiet lane set — that answers it, not its first.

   One budget ROSE: `blocking-composite` 277 → 341, exactly one allocation per
   operation over a batch of 64. Bisected across the 327 commits since the pin
   was taken, to `66c2f156` — "a blocking body's result keeps its width". The
   row was cheaper before because it was WRONG: the composite result was cut
   into a word, and the three commits before the fix cannot run the fixture at
   all, panicking `VM1101: integer overflow` in `sum64`.

   `epic_base` moved to `4968061f`, and the cost is stated rather than hidden:
   the base compiler at the frozen commit refuses today's fixtures outright
   (`'ret' is not supported inside async/blocking payloads`), so no run of any
   kind reached the candidate side. The allocation half is candidate-only and is
   unaffected. **The relative-performance half now asserts nothing** — base and
   candidate are one tree — and the twenty rows carrying `relative_performance`
   are green by identity until a base worth comparing against exists.
4. ~~**The cancelled-scope reclamation defect**~~ — **LANDED** as `9c7b9b3e`,
   closing the ledger row that had proposed exactly this fix from a code
   reading and never reproduced it.
6. **The allocation test** the clone ruling obliges: every generated
   move/copy/clone body tests its `rt_alloc` and panics naming the type. Lands
   in W4's files, so it follows W4. *~2 days.*
7. **W5 — D8's adopt leg**, after W4 is integrated. *~1 day.*
8. **W8 — wave closeout** when D2, D7, D8, D3b and the cancellation window are
   all closed on one tree. *~1 day.*

**Wave D's exit condition, restated 2026-08-29 against what was measured.** The
first form asked for three categories at live zero and for the whole remainder
to sit in `rt_remote_task_*` and `rt_far_channel*`. Both halves were wrong, in
opposite directions, and both are corrected here rather than declared met.

The FIRST half asked one category for a number it cannot reach.
`llvm-pointer-word-ir`'s last finding is `emit_term.go`'s
`inttoptr (i64 N to ptr)` — an LLVM CONSTANT EXPRESSION building the fixnum
tagged immediate from a compile-time constant, carrying the reviewed permanent
allowance `fixnum-inline-tagged-word`, which retires only if fixnums stop using
pointer-typed tagged immediates. That is Epic 22's representation, not this
wave's, and spelling the same reinterpretation another way to move the count
would be gaming the instrument.

The SECOND half asked the remainder to be confined to two file families, and
this document's own §5 already exempts nine of those findings from every wave.
It also mislocated Wave E: `numeric-drop-dispatch` and `native-word-carrier`
live partly in the CROSSING EMITTERS (`emit_async.go`, `emit_channel.go`,
`emit_crossing_*.go`) and in `rt_async_internal.h`, not only in the far files —
seven findings, all Wave E's by category and none of them Wave D's.

**So Wave D closes when, and this is checkable:**

| what | required | measured 2026-08-29 |
| --- | ---: | ---: |
| `suspension-frame-owner` | 0 | **0** |
| `llvm-erased-word-bridge` | 0 | **0** |
| `llvm-pointer-word-ir` | 1 allowed | **1** |
| everything else | Wave E's by category, or named in §5 as no wave's | 42 + 17 |

and D2 is closed by measurement, and the aggregate roster is green as a COUNT OF
RUNS rather than one exit code.

**The carrier half is met. The wave is not closed until the last two are.** `llvm-erased-word-bridge` reached zero on 2026-08-29 and
`llvm-pointer-word-ir` reached its one; the one is `emit_term.go`'s
`inlineFixnumWord`, an LLVM constant expression building the tagged immediate
`fixi_box` builds, held by the reviewed permanent allowance
`fixnum-inline-tagged-word`. Raw zero for that category means the fixnum
representation has changed, which is what the allowance's `invalidated_when`
names and is nobody's work in this wave; the condition was written before anyone
re-read the site, and this is the re-read.

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

## After Wave D

Taken in order, not in parallel with the above.

| | What | Size | Closes when |
| --- | --- | ---: | --- |
| **Wave E** | Far carriers, leases, byte credits — six items, all in `rt_remote_task_*` and `rt_far_channel*`. Transport rulings already given 2026-08-28. | ~12 | `native-payload-bits`, `native-word-carrier` and `numeric-drop-dispatch` read live zero, and saturation is proven to park a producer without busy retry. |
| **Wave F** | Diagnostics, then deletion of the legacy symbols, then the closeout gates. | ~8 | The census reads **0 in every category a wave owned** (owner ruling 2026-09-03, variant (а): of the manifest's frozen 683 on `7df10725`, the 55 that remain live are the VM's `Value` interchange type -- `vm-universal-owner` 40, plus the 27 RV2-DEBT-318 migration paths -- and the async runtime's `any` -- `vm-async-any-carrier` 14 -- with the one permanent `fixnum` allow; those are counted and pinned by `TestLiveCarrierRatchetAgainstRepository`, and their removal is a VM-representation epic of its own, not this wave's; "0 of 683" as first written asked for that epic), the symbols are deleted rather than unused, and `make runtime-v2-check` reports every sub-gate on its roster passing on one tree (the roster is the Makefile's `RUNTIME_V2_SUBGATES`, twenty today, and the three Epic 21 e2e proofs ride inside `runtime-v2-crossing-check` rather than as rows of their own). |
| **Epic 22 ph. 2** | `int`/`uint` reclamation. | ~5 | — |
| **Epic 21** | Bench, matrix, seam, debt closeout. | ~2 | — |

Sizes are lane-days: one focused worker, one day. Wave D's tail is ~12 more, W2 having been measured out.
**~39 lane-days remain — about six calendar weeks at three lanes, eight with a
blocker allowance**, which the history justifies: this migration has hit one
parking blocker per epic.

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
