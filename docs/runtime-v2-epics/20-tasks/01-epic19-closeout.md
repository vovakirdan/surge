# Epic 20 Task 1: Epic 19 Closeout (bench + debt + durable-doc sync)

Executes the never-run Epic 19 Task 5. Docs + bench only; no code.

## Rows

1. **Bench — reclaim model vs leak model, steady-state cost.**
   Compare the committed pre-drop tree `c5748852` (Epic 19 kickoff
   baseline commit, drops not yet emitted) against HEAD on
   allocation-hot workloads where scope-exit drops now run every
   iteration:
   - string workload: loop-built strings (literals are static — the
     RV2-DEBT-048 probe lesson — so payloads must be runtime-built);
   - array workload: per-iteration dynamic array with push.
   Metrics per build: wall time (>=5 runs, median), peak RSS
   (`/usr/bin/time -v`), and valgrind total allocs/frees at reduced N
   (the census contrast: leak model frees ~nothing, reclaim model
   balances). Both builds must print the same checksum (execution
   witness, RV2-DEBT-049).
2. **Epic 19 record closed:** `19-drop-emission.md` status flips from
   IN EXECUTION to CLOSED with a dated note pointing here and at
   `20-crossing-drop-activation.md` for vertical 2.
3. **`RUNTIME_V2.md` Phase 4 status sync:** the status paragraph
   still claims remote channels, remote select, distributed scopes,
   migration are future work; reword to reflect Epics 14 (remote
   channels), 16 (share leases), 17 (remote select, single-owner
   Model C), 18 (migration capture lift), and that Epic 20 activation
   is IN PROGRESS (must not read as done). Pool execution, distributed
   scopes, credits, VM transport stay future.
4. **DEBT ledger touch-ups:** RV2-DEBT-034 row notes the language
   gate is lifted (Epic 19 shipped compiled glue) and activation is
   owned by Epic 20; no rows closed here.
5. **Gate:** `make check` on the final tree.

## Evidence (2026-07-17, reference host WSL2, LLVM release builds)

Three committed trees benched: `c5748852` (Epic 19 kickoff, leak
model), `7d7f5230` (last Epic 19 commit: drops on, pre-fixnum),
current HEAD (drops + fixnum + hardening sweep). Two programs, each
printing a checksum as the execution witness (identical across all
builds): string workload (100k iterations, loop-built string of 8
concats — literals are static and never heap-allocate, so payloads
are runtime-built) and array workload (100k iterations, 16-element
`push` array per iteration). 5 runs each via `/usr/bin/time -f "%e %M"`.

| Build | str wall (median) | str peak RSS | arr wall (median) | arr peak RSS |
| --- | --- | --- | --- | --- |
| kickoff `c5748852` (leak) | 0.63 s | 168.8 MB | 1.34 s | 328.3 MB |
| drops `7d7f5230` | 0.70 s | 129.7 MB | 1.38 s | 312.7 MB |
| HEAD (drops+fixnum) | 0.59 s | 1.7 MB | 1.11 s | 1.7 MB |

Attribution:

- **Drop emission alone** (`7d7f5230` vs kickoff): +11% / +3% wall
  time on these allocation-extreme micro loops; RSS down where drops
  apply (−23% / −5%). The residual hundreds of MB at the drops tree
  is the pre-fixnum int/uint churn (`Copy` bignums per loop-counter
  increment — the RV2-DEBT-035/036 era), not string/array leaks.
- **The full vertical** (HEAD vs kickoff): NET FASTER than the leak
  model (−6% str, −17% arr) with flat ~1.7 MB peak RSS on both
  workloads. Reclaim + inline ints beat leaking on time, not only on
  memory.

Valgrind census contrast, string workload (total heap usage):

| Build | allocs | frees | balance |
| --- | --- | --- | --- |
| kickoff (leak) | 7,800,037 | 2,800,004 | 5.0M blocks never freed |
| drops | 8,600,033 | 4,500,010 | 4.1M unfreed = int churn |
| HEAD | 6,500,021 | 6,500,021 | EXACT balance |

Verdict for the Epic 19 question ("what does the reclaim model cost
at steady state vs the leak model"): the drop machinery alone costs
single-digit-to-~11% wall time on worst-case allocation loops, and
the completed arc (with fixnum) is net faster with exactly balanced
alloc/free. No regression to record; RV2-DEBT-040 (for-loop iterator
leak) and 035/036/038 remain the ledgered reclamation tails.

Docs synced in this task: `19-drop-emission.md` → CLOSED;
`RUNTIME_V2.md` Phase 4 status paragraph updated to the Epics 14-20
reality; RV2-DEBT-034 row notes the lifted language gate, owner moved
to Epic 20, and the owned-results reply-edge obligation was added to
the epic's Task 5 text. Gate: `make check` PASSED (exit 0,
2026-07-17) on the final tree. Bench worktrees removed.

## Status

COMPLETE (2026-07-17). Epic 19 is fully closed; the reclamation arc's
vertical-1 record is whole.
