# Runtime V2 — the board

What is being worked right now, what is taken next, and in what order. Updated
IN PLACE, not appended to: this file is a board, not a log.

It does not restate the migration. `README.md` holds the epic history and
roadmap, `DEBT.md` the ledger, `23b-*.md` the wave definitions, `RULES.md` the
rules every lane obeys. Read those to know why; read this to know what.

**State line, 2026-08-28 at `0001b82c`.** Live carrier census **83 against a
frozen base of 626**, ratchet green. Epics 1–25 are closed except three: 21
(closeout only), 22 (parked until 23b closes), 23b (in flight — the whole
remaining migration).

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

| Lane | Branch / where | State | What must happen |
| --- | --- | --- | --- |
| **Scope join fix** | trunk, `48285f25` | **Landed.** A scope had stopped counting any child, so it answered before its children finished — my own regression in the claim protocol. Losing the claim to this scope's OWN id is now a win. | Aggregate gate on the dedicated machine, then done. |
| **W4 · frame leak** | LANDED on trunk | Committed. Critic: SOUND_WITH_GAPS. It REFUTED the frame leak it was sent to find — the frame is released on every path — and found a real one instead: a task cancelled before its first poll never gives back its `rt_scope` block, 1.00 block / 64.0 bytes per cancelled task. | Fix the three things the critic named: the row turns two green gates red and the report does not say so; a second unreported retention at four workers; a false claim about `make check` written into a source comment. |
| **W4 · frame word** | `lane-d7-2` | Committed. Critic: **UNSOUND**. The blocking frame's word is wrong by construction — no write site exists in `lower_blocking.go`, so a blocking frame is born PACKED and never corrected. Half the write sites have no failing test; the construction site pins the count, not the word. | Rework, not repair. The field is right; its writers are not. |
| **W7 · channel** | `lane-d7-3` | Committed. Critic: SOUND_WITH_GAPS. Pins land with a real negative row; the §7 teardown half ships with no failing test, the header enumerates pin sites that do not exist, and `dying` is a load-then-store pair rather than a gate. | Give the teardown its failing test, fix the header's enumeration, and make `dying` one read-modify-write. |
| **Scope refusal** | `lane-scope-refusal` | Committed. Critic: **UNSOUND**. Refuses a legal program with a false message; three bypasses compile silently (alias, sync helper, call pass-through) and two of them give OPPOSITE answers. Its justification for deleting the runtime adoption is false — a sync-helper program still reaches `rt_task_wake` with a scope set. | Rework. The refusal must ask about the TASK, not the spelling of the binding. |
| **W2 instrument** | dedicated machine | **DONE: 0 red in 800 runs.** At the ~0.5% rate the row was chasing, zero in 800 has probability 1.8%. The entry condition for the fourth-window lane is NOT met. | Nothing. W2 is off the schedule until an instrument produces a red. |

## Next, in this order

1. **The two SOUND_WITH_GAPS lanes get their gaps closed and land** — the frame
   leak (`lane-d7-1`) and the channel pins (`lane-d7-3`). Both have real,
   measured findings; neither is integrable while its critic's list stands.
2. **The two UNSOUND lanes are re-sent, not repaired.** `lane-d7-2`'s field is
   right and its writers are not; `lane-scope-refusal` asks about the spelling
   of a binding where it must ask about the task. Each re-sent lane carries its
   critic's findings as part of the brief.
3. **W6 — D2's measurement close. BLOCKED, and both blockers are new.** Moving
   `epic_base` forward works: the base compiler at the frozen commit could no
   longer compile the fixtures at all, and with the base moved the bench
   measures again. Four budgets were re-captured by the instrument itself —
   `select-send-composite` 134 → 6, `task-clone-composite` 405 → 277,
   `far-channel-composite` 474 → 410, `far-select-composite` 479 → 415 — typed
   carriers having removed a box the budgets were never re-captured for. Then:
   - `--phase=final` **cannot pass until Wave E exists**. Its liveness probes
     wait on the sync point `SP_CARRIER_CREDIT_PARKED`, and the manifest's own
     deferral reason says "Wave E must reach the frozen sync point". The probe
     does not fail — it TIMES OUT at 10s waiting for a mechanism nobody built.
     It also needs re-deriving against the 2026-08-28 transport ruling, under
     which pointer transport takes no byte credits at all.
   - `--phase=wave-a` **needs a quiet machine**. It aborts on
     `array-grow-composite base p95 CV 0.415389 exceeds 0.050000` — eight times
     the allowance, measured while lanes and a gate were running. The allocation
     half is deterministic and does not care; the latency half cannot be taken
     on a busy host.
   - Two rows **carry no single number**: `channel-unbuffered-composite` and
     `-scalar` read 4 in warmup and 3 in every measured pair, repeatably. An
     exact allocation budget presumes determinism and these do not have it — the
     first batch pays a one-time cost the rest do not. That is a question about
     the measurement window or the fixture, and it is the owner's.
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

---

## After Wave D

Taken in order, not in parallel with the above.

| | What | Size | Closes when |
| --- | --- | ---: | --- |
| **Wave E** | Far carriers, leases, byte credits — six items, all in `rt_remote_task_*` and `rt_far_channel*`. Transport rulings already given 2026-08-28. | ~12 | `native-payload-bits`, `native-word-carrier` and `numeric-drop-dispatch` read live zero, and saturation is proven to park a producer without busy retry. |
| **Wave F** | Diagnostics, then deletion of the legacy symbols, then the closeout gates. | ~8 | The census reads **0 of 626**, the symbols are deleted rather than unused, and `make runtime-v2-check` reports twenty passes on one tree. |
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
| `vm-universal-owner`, 7 live | The VM's `Value` frame slot. Neither wave's owner; a VM representation change with no epic. |
| `vm-async-any-carrier`, 2 live | `heap.Interface` in `internal/asyncrt/timer.go`. Not a carrier the model describes. |
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
