# Block 04: Crossing Contracts

Block 4 closes the crossing contract layer after `far`, `on`, and `spawn on`.
There is no public `crosses` keyword: crossing is an inferred semantic effect
recorded by sema as `Result.FunctionEffects[fn].MayCross`.

## Confirmed Contract

- `on dst { ... }` marks the enclosing function `MayCross`.
- `spawn on dst { ... }` marks the enclosing function `MayCross`.
- `far Task<T>.await()` and `far Task<T>.cancel()` mark the enclosing function
  `MayCross`.
- Direct calls to functions inferred as `MayCross` propagate the effect through
  the caller chain.
- `crosses` is an ordinary identifier outside normal grammar positions. It is
  not a keyword, not an attribute, and not a function-type modifier.
- `@shard_movable` and `@shard_pinned` are type attributes only.
- `@shard_movable` and `@shard_pinned` conflict on the same type.
- Owned user-defined values may cross an `on` / `spawn on` boundary only when
  their nominal type is `@shard_movable`.
- `@send` is not enough for owned shard movement.
- `@copy` is not enough for owned shard movement. It allows copying, not
  resource migration.
- `@shard_pinned` resources may be represented by `far T` handles, but the
  resource value itself cannot cross as `own T`.
- `@nosend` values may be captured by `@local spawn`; ordinary `spawn` keeps the
  existing `SEM3086` diagnostic, while `on` / `spawn on` use the crossing-family
  diagnostic `SEM3166`.

## Retired Codes

These numbers are reserved and must not be reused:

| Code | Retired Meaning |
| --- | --- |
| `SEM3162` | Function performs crossing work but is not marked `crosses`. |
| `SEM3163` | Non-`crosses` function calls a `crosses` function. |
| `SEM3164` | `far Task<T>.await()` / `.cancel()` outside `crosses`. |
| `SYN2034` | Invalid placement of `crosses`. |
| `SYN2035` | `crosses` on a non-function target. |
| `SYN2036` | `crosses fn(...) -> T` function-type syntax. |

## Active Diagnostics

| Code | Meaning |
| --- | --- |
| `SYN2016` | `@shard_movable` or `@shard_pinned` used on a non-type target. |
| `SEM3172` | `@shard_movable` and `@shard_pinned` both appear on one type. |
| `SEM3171` | `@shard_movable` type contains a non-shard-movable field/member. |
| `SEM3169` | Owned `@send` user type crosses without `@shard_movable`. |
| `SEM3170` | Owned `@copy` user type crosses without `@shard_movable`. |
| `SEM3165` | Borrowed value crosses an `on` / `spawn on` boundary. |
| `SEM3166` | `@nosend` value crosses an `on` / `spawn on` boundary. |
| `SEM3167` | Owned `@shard_pinned` value crosses an `on` / `spawn on` boundary. |
| `SEM3168` | Unmarked owned user value crosses an `on` / `spawn on` boundary. |
| `SEM3086` | Existing ordinary `spawn` sendability violation for `@nosend`. |
| `SEM3174` | `@local spawn on` mixes local-only spawn with remote placement. |

## Effect Matrix

| Fixture | Case | Expected |
| --- | --- | --- |
| `effect_positive_on_inferred.sg` | `on dst { ... }` in a plain function | Valid; sema infers `MayCross`. |
| `effect_positive_spawn_on_inferred.sg` | `spawn on dst { ... }` in a plain function | Valid; sema infers `MayCross`. |
| `effect_positive_far_task_await_inferred.sg` | `far Task<T>.await()` | Valid; sema infers `MayCross`. |
| `effect_positive_far_task_cancel_inferred.sg` | `far Task<T>.cancel()` | Valid; sema infers `MayCross`. |
| `effect_positive_call_chain_inferred.sg` | `outer -> middle -> inner(on ...)` | Valid; sema propagates `MayCross`. |
| `effect_positive_crosses_identifier.sg` | `let crosses = ...` | Valid; `crosses` is an identifier. |

Unit coverage in `internal/sema/crossing_effect_test.go` asserts the actual
`FunctionEffects.MayCross` metadata. The golden fixture harness only proves
that the current language surface compiles without requiring a marker keyword.

## Attribute And Capture Matrix

| Family | Fixtures | Contract |
| --- | --- | --- |
| Attribute targets | `attr_negative_*` | Shard attributes are type-only (`SYN2016`). |
| Attribute conflict | `attr_negative_movable_pinned_conflict.sg`, `pinned_negative_movable_conflict.sg` | Movable and pinned are mutually exclusive (`SEM3172`). |
| Movable fields | `movable_positive_*`, `movable_negative_*` | Movable fields may be primitives, `far T`, arrays of movable values, or explicitly movable user types; unmarked, pinned, or nosend fields fail (`SEM3171`). |
| Copy captures | `capture_positive_copy_*` | Builtin Copy captures cross by copy. |
| Owned movable captures | `capture_positive_shard_movable_*`, `locality_positive_*_movable_on.sg` | Owned user values cross only through `@shard_movable`. |
| Far handle captures | `capture_positive_far_handle_on.sg`, `pinned_positive_on_far_handle.sg` | `far T` moves the handle while the resource stays on its owner shard. |
| Nosend local spawn | `capture_positive_nosend_local_spawn.sg`, `locality_positive_nosend_local_spawn.sg` | `@local spawn` remains the local escape hatch. |
| Nosend remote crossing | `capture_negative_nosend_on.sg`, `capture_negative_nosend_spawn_on.sg`, `locality_negative_nosend_*` | `on` / `spawn on` reject `@nosend` (`SEM3166`); ordinary `spawn` keeps `SEM3086`. |
| Pinned owned crossing | `capture_negative_pinned_*`, `pinned_negative_owned_*` | Owned pinned resources do not migrate (`SEM3167` for `on` / `spawn on`, `SEM3086` for ordinary spawn when also `@nosend`). |
| Send/copy only | `locality_negative_send_only_*`, `locality_negative_copy_only_*` | `@send` and `@copy` alone do not permit owned shard movement (`SEM3169`, `SEM3170`). |

## Implementation Notes

- Lowering must consume sema `FunctionEffects`, not parser/AST syntax.
- The explicit `crosses` grammar and `Signature.Crosses` field were deleted in
  design change D17.
- `core/intrinsics.sg` marks both `TcpConn` and `TcpListener` as
  `@shard_pinned @nosend`.
- The current inference is intra-sema-result and direct-call based. Exporting
  function effects across module boundaries is tracked separately if Phase 4
  lowering needs cross-module caller effects.

## Completion Gates

- `go test ./internal/sema -run 'TestFunctionCrossingEffectInference|TestOnCrossingDiagnostics|TestSpawnOnDiagnostics' -count=1`
- `go test ./internal/crossinggate -run TestEpic11Block4Contracts -count=1`
- `go test ./internal/parser -run 'TestCrosses|TestOnCrossing|TestSpawnOn' -count=1`
- `make golden-check` after active fixture/golden regeneration.
