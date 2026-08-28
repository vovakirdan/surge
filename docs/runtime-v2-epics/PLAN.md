# Runtime V2 — the board

What is being worked right now, what is taken next, and in what order. Updated
IN PLACE, not appended to: this file is a board, not a log.

It does not restate the migration. `README.md` holds the epic history and
roadmap, `DEBT.md` the ledger, `23b-*.md` the wave definitions, `RULES.md` the
rules every lane obeys. Read those to know why; read this to know what.

**State line, 2026-08-28 at `ace94c47`.** Live carrier census **83 against a
frozen base of 626**, ratchet green. Epics 1–25 are closed except three: 21
(closeout only), 22 (parked until 23b closes), 23b (in flight — the whole
remaining migration). Trunk is green: `runtime-v2-lifecycle-check`,
`-heap-check` and `-carrier-sanitizer-check` all exit 0 on the dedicated machine
(183s / 135s / 99s, 77 lifecycle rows, 0 failures). No scheduled work waits on
an owner decision.

---

## In flight

| Lane | Branch / where | Doing | Done when |
| --- | --- | --- | --- |
| **W4 · frame leak** | `lane-d7-1` | Exhibiting the async-frame leak RED with a byte/block number, wired as a NAMED row in the sanitizer gate. Committed, in adversarial review. | The row is red with a recorded number, and it runs under its gate rather than alone. |
| **W4 · frame word** | `lane-d7-2` | The MIR frame lifecycle field written truthfully at pack, unpack and both returns; `AsyncStateFreeBuiltin` deleted in the same commit. Committed, in review. | The field is read by something, no dual path remains, `make golden-check` green. |
| **W7 · channel** | `lane-d7-3` | Channel pins — zero callers today, so the fail-closed reclaim gate is vacuous — and §7's teardown order in `rt_channel_free`. Committed, in review. | A program that would have reclaimed under a waiter is refused, and the teardown performs §7's order. |
| **Scope refusal** | `lane-scope-refusal` | Sema refuses the `spawn` a scope partially adopts; the dead adoption block in `rt_task_wake` goes with it. Uncommitted. | The refusal has its own diagnostic and a golden `.diag`, shown red on the revert. |
| **W2 instrument** | dedicated machine | 200 runs of the gate's own cancellation line, to establish whether the ~0.5% residue is alive. | A count of reds out of 200. One green run answers nothing. |

## Next, in this order

1. **Integrate the four lanes** as their critics report. W4's two go first: W5
   must follow them, and W2 and W3 cannot share `rt_task_complete.c` with them.
2. **W6 — D2's measurement close.** No conflicts, no lane needed. Re-capture the
   frozen bench manifest against the latest green commit, then re-measure with
   at least three agreeing runs. *~1 day. Closes D2, which has been code
   complete and unmeasured for weeks.*
3. **W2 — the fourth cancellation window**, only if the campaign produces a red
   under the gate. Zero reds means the entry condition is not met and the row is
   re-measured, not worked. *~3 days if entered.*
4. **The allocation test** the clone ruling obliges: every generated
   move/copy/clone body tests its `rt_alloc` and panics naming the type it could
   not duplicate. Today the body tests nothing and `rt_alloc` returns `NULL`
   without panicking, so a refused allocation is a segmentation fault. Lands in
   W4's files, so it follows W4. *~2 days.*
5. **W5 — D8's adopt leg.** Typed return for the select winner, delete
   `emitI64ToValue`. *~1 day, after W4 is integrated.*
6. **W8 — wave closeout** when D2, D7, D8, D3b and the cancellation window are
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
