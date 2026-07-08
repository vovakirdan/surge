# crosses_deferred — parked crossing-effect scenarios

These fixtures are **parked, not deleted**. They exercise the crossing-effect
scenarios that the explicit `crosses` keyword used to check at parse/sema time.
The keyword was removed on 2026-07-08 (design change D17); the crossing effect is
now to be **inferred at semantic analysis** and stored in function metadata, with
the inference implementation deferred to the Phase 4 transport epic
(see `docs/runtime-v2-epics/DEBT.md` RV2-DEBT-024).

Until inference lands there is nothing to assert, so these files are:

- **NOT wired into** the `internal/crossinggate` harness — it scans only
  `block0{1..4}/{valid,invalid}`, never this directory; and
- **skipped by** `scripts/golden_update.sh` — this directory is pruned, so no
  sidecars are generated and the `README.md` is preserved.

Each file carries a `FUTURE-ASSERT:` note describing what semantic
crosses-inference should assert once implemented; none carry an `EXPECT-DIAG`
header (the surface diagnostics they used to assert are retired). The retired
surface diagnostics `SEM3162`/`SEM3163`/`SEM3164` keep their reserved numbers.

| File | Origin | Future assertion |
| --- | --- | --- |
| `_spawn_on_negative_without_crosses.sg` | Block 3 X03 (was `SEM3162`) | fn containing `spawn on` gains an inferred crossing effect in its metadata |
| `_spawn_on_negative_crosses_call_propagation.sg` | Block 3 X04 (was `SEM3163`) | inferred crossing effect propagates through calls (caller of a crossing fn is itself inferred crossing) |
| `_spawn_on_negative_await_without_crosses.sg` | Block 3 T07 (was `SEM3164`) | `far Task<T>.await()` implies a crossing effect on the enclosing fn |
| `_spawn_on_negative_cancel_without_crosses.sg` | Block 3 T08 (was `SEM3164`) | `far Task<T>.cancel()` implies a crossing effect on the enclosing fn |
| `on_negative_missing_crosses.sg` | Block 2 ON-CROSS-N001 (was `SEM3162`) | assert inferred crosses metadata on the enclosing fn containing `on` |
