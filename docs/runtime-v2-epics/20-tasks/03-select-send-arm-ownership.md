# Epic 20 Task 3: Local Select Send-Arm Ownership (RV2-DEBT-048)

Soundness fix: a winning local select send arm double-frees its
payload (receiver drop + sender scope-exit drop) because the arm is
invisible to move tracking; use-after-move compiles. Parallel lane —
no dependency on the activation chain.

## Semantics (per the resolved epic fork 6, local half)

Per-branch move, reusing the compare-expression machinery verbatim:

- In the WINNER branch of a send arm the payload is moved (the
  channel delivered it; the receiver owns it).
- Inside every OTHER arm's body the payload is still owned and
  usable (Go-shaped retry loops keep working).
- After the select join the payload is maybe-moved: any use is a
  use-of-moved error; the union moved-set means scope exit emits no
  drop; each non-mover arm reclaims via per-arm drop synthesis
  (`ArmDropsExpr` → HIR `wrapArmDrops`).

Spelling: a non-copy payload requires the same `own` marker direct
send requires (`ch.send(own s)`); Copy payloads stay bare. The
payload must be a whole binding: temporaries and projections
(fields/index reads) get a kind diagnostic telling the programmer to
bind the value first — the per-branch reclamation contract is
binding-based. Far-channel send arms are untouched here (remote
symmetry is Task 7).

Retry-loop shape (resolved during implementation): break/continue are
`returnOpen` in flow status, so a payload declared OUTSIDE the loop
and sent-with-`break` inside it hits the existing conservative
back-edge rule (`rejectLoopBackEdgeMoves`) with its standing hint —
"move it outside the loop or recreate it each iteration". The
accepted Go-shaped form declares the payload INSIDE the loop: the
continue arm reclaims it via per-arm drop synthesis, the break path
delivered it, the next iteration builds a fresh one, and every path
frees exactly once. True Go parity (reusing the same binding across
iterations after a lost send) needs runtime drop flags, rejected as
an Epic 19 fixed point; recorded as the model's ceiling, not a bug.

## Mechanics (verified starting points)

- `typeSelectAwaitExpr` hand-types send arms and bypasses overload
  resolution, ownership application, and payload assignability
  (`internal/sema/type_expr_select.go:288-297`) — the root cause.
- Direct send gets ownership via `applyParamOwnership` →
  `observeMove` (`internal/sema/magic_ownership.go:12-37`,
  `borrow_runtime_ops.go:12`).
- Compare exprs already do per-arm snapshots + union merge + one-sided
  droppables → `ArmDropsExpr` (`internal/sema/type_expr_flow.go`);
  HIR wraps tagged arm results into `{ @drop ...; ret v }` blocks
  (`internal/hir/lower_expr.go:47-70`) — select arm results lower
  through `l.lowerExpr(arm.Result)` so the tag rides for free
  (`internal/hir/lower_expr_control.go:43`).

## Rows (test-first)

1. Goldens invalid: bare non-copy `ch.send(s)` in an arm → new
   `SEM3140` (own marker required, hint spells `send(own s)`);
   temporary/projection payload → new `SEM3141` (bind it first);
   payload type mismatch vs channel payload → `SEM3015`; use after
   the select join → existing use-of-moved diagnostic.
2. Goldens valid: `own` binding form; payload used inside a losing
   arm's body; the retry-loop shape (`loop { select { ch.send(own j)
   => break; t => continue } }`).
3. E2e reclamation (VM + LLVM, runtime-built payloads, execution
   witnesses; wired into `runtime-v2-heap-check`): winner path frees
   exactly once (the RV2-DEBT-048 double-free program runs clean);
   loser path reclaims at arm end (definitely-lost 0 for the
   payload); copy payloads unaffected.
4. `make check` + golden regen byte-stable + Sentrux committed-tree
   comparison.

## Evidence (2026-07-17)

- Sema: `typeSelectAwaitExpr` send case now runs
  `checkSelectSendPayloadOwnership` for local channels (assignability
  vs channel payload; `own` marker → SEM3140; whole-binding →
  SEM3141; `observeMove` into the current arm's snapshot);
  `typeSelectExpr` mirrors the compare-expression per-arm machinery
  (snapshot/restore per arm, `compareArmAbruptExit`, union merge,
  `ArmDropsExpr` one-sided drops). HIR/MIR/backends unchanged —
  `wrapArmDrops` picked the tags up as designed.
- Goldens (all regenerate byte-stable, zero drift outside the new
  files): SEM3140 missing-own, SEM3141 temp payload, SEM3015 payload
  type mismatch, SEM3130 use-after-join, SEM3130 back-edge for the
  declared-outside retry shape; valid file covers losing-arm use and
  the declare-inside-loop retry form.
- E2e: `TestRuntimeV2DropSelectSendArm` (llvm, `SURGE_THREADS=1,2`)
  pins exact free-count windows — winner: select 2 / recv 0 /
  drop-got 2 (payload + Option box exactly once, at the receiver);
  loser: select 3 (in-arm reclaim). Wired into
  `runtime-v2-heap-check`. Sync-main + `spawn run()` entry shape
  (async entry hits RV2-DEBT-049 startup loss and RV2-DEBT-050
  swallowed exit codes; the driver retries only on the exact 049
  signature with a loud log).
- VM parity: the same scenario runs marker-clean under the VM
  sanitizer (no double free / UAF / leak panic); VM heap-stats window
  DELTAS differ from LLVM by runtime internals, so census numbers
  stay LLVM-gated by design.
- Pre-fix reproducer (3/3 native `free(): double free detected in
  tcache 2` at `9b8b6d2d`) runs clean; manual valgrind winner+loser
  0 definitely-lost.
- Gates: `make check` green; `make golden` green; Sentrux `internal`
  6484 → 6486 (above the committed baseline).
- Incidental bugs ledgered during the task: RV2-DEBT-049 escalated
  (NATIVE startup loss of the async entry task, trace-proven:
  `tasks_done=0`, `worker_sleep=32`, `wake_called=0`, exit 0) and
  RV2-DEBT-050 opened (async entrypoint return code swallowed).

## Status

COMPLETE (2026-07-17). RV2-DEBT-048 closed. Remote symmetry for far
send arms remains Task 7 on the Task 3 semantics.
