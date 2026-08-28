# Runtime V2 — the board

What is being worked right now, what is taken next, and in what order. Updated
IN PLACE, not appended to: this file is a board, not a log.

It does not restate the migration. `README.md` holds the epic history and
roadmap, `DEBT.md` the ledger, `23b-*.md` the wave definitions, `RULES.md` the
rules every lane obeys. Read those to know why; read this to know what.

**State line, 2026-08-28 at `48285f25`.** Live carrier census **83 against a
frozen base of 626**, ratchet green. Epics 1–25 are closed except three: 21
(closeout only), 22 (parked until 23b closes), 23b (in flight — the whole
remaining migration). Trunk is green: `runtime-v2-lifecycle-check`,
`-heap-check` and `-carrier-sanitizer-check` all exit 0 on the dedicated machine
(183s / 135s / 99s, 77 lifecycle rows, 0 failures). No scheduled work waits on
an owner decision.

---

## In flight

| Lane | Branch / where | State | What must happen |
| --- | --- | --- | --- |
| **Scope join fix** | trunk, `48285f25` | **Landed.** A scope had stopped counting any child, so it answered before its children finished — my own regression in the claim protocol. Losing the claim to this scope's OWN id is now a win. | Aggregate gate on the dedicated machine, then done. |
| **W4 · frame leak** | `lane-d7-1` | Committed. Critic: SOUND_WITH_GAPS. It REFUTED the frame leak it was sent to find — the frame is released on every path — and found a real one instead: a task cancelled before its first poll never gives back its `rt_scope` block, 1.00 block / 64.0 bytes per cancelled task. | Fix the three things the critic named: the row turns two green gates red and the report does not say so; a second unreported retention at four workers; a false claim about `make check` written into a source comment. |
| **W4 · frame word** | `lane-d7-2` | Committed. Critic: **UNSOUND**. The blocking frame's word is wrong by construction — no write site exists in `lower_blocking.go`, so a blocking frame is born PACKED and never corrected. Half the write sites have no failing test; the construction site pins the count, not the word. | Rework, not repair. The field is right; its writers are not. |
| **W7 · channel** | `lane-d7-3` | Committed. Critic: SOUND_WITH_GAPS. Pins land with a real negative row; the §7 teardown half ships with no failing test, the header enumerates pin sites that do not exist, and `dying` is a load-then-store pair rather than a gate. | Give the teardown its failing test, fix the header's enumeration, and make `dying` one read-modify-write. |
| **Scope refusal** | `lane-scope-refusal` | Committed. Critic: **UNSOUND**. Refuses a legal program with a false message; three bypasses compile silently (alias, sync helper, call pass-through) and two of them give OPPOSITE answers. Its justification for deleting the runtime adoption is false — a sync-helper program still reaches `rt_task_wake` with a scope set. | Rework. The refusal must ask about the TASK, not the spelling of the binding. |
| **W2 instrument** | dedicated machine | **0 red in 200 runs.** Not "fixed": at a 0.5% rate, zero in 200 happens 37% of the time. | 600 more runs. Zero in 800 puts the rate under ~0.4% with 95% confidence; a red enters W2. |

## Next, in this order

1. **The two SOUND_WITH_GAPS lanes get their gaps closed and land** — the frame
   leak (`lane-d7-1`) and the channel pins (`lane-d7-3`). Both have real,
   measured findings; neither is integrable while its critic's list stands.
2. **The two UNSOUND lanes are re-sent, not repaired.** `lane-d7-2`'s field is
   right and its writers are not; `lane-scope-refusal` asks about the spelling
   of a binding where it must ask about the task. Each re-sent lane carries its
   critic's findings as part of the brief.
3. **W6 — D2's measurement close.** No conflicts, no lane needed, and the
   machine is free between campaigns. Re-capture the frozen bench manifest
   against the latest green commit, then re-measure with at least three agreeing
   runs. *~1 day. Closes D2, code complete and unmeasured for weeks.*
4. **The cancelled-scope reclamation defect** the leak lane found: `run_ready_one`
   is a hand-copy of `apply_poll_outcome`'s switch whose cancelled arm performs
   no scope teardown, so a task cancelled before its first poll never gives back
   its scope block. One block and 64 bytes per cancelled task, measured. This is
   a live unbounded retention and it blocks nothing else, so it follows the
   lanes above rather than preempting them. *~1 day.*
5. **W2**, only if the 800-run campaign produces a red under the gate. Zero in
   800 means the entry condition is not met and the row is re-measured, not
   worked. *~3 days if entered.*
6. **The allocation test** the clone ruling obliges: every generated
   move/copy/clone body tests its `rt_alloc` and panics naming the type. Lands
   in W4's files, so it follows W4. *~2 days.*
7. **W5 — D8's adopt leg**, after W4 is integrated. *~1 day.*
8. **W8 — wave closeout** when D2, D7, D8, D3b and the cancellation window are
   all closed on one tree. *~1 day.*

**Wave D closes when** `suspension-frame-owner`, `llvm-erased-word-bridge` and
`llvm-pointer-word-ir` read live zero and the remainder is confined to
`rt_remote_task_*` and `rt_far_channel*`. That is 24 of the 83 live findings.

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

Sizes are lane-days: one focused worker, one day. Wave D's tail is ~15 more.
**~42 lane-days remain — about six calendar weeks at three lanes, eight with a
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
