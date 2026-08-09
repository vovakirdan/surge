# Epic 11 Task Blocks

This directory holds the documentary test-case matrices and fixture inventories
for Epic 11 (explicit crossing language surface). Each block document is
documentary only: it records parser, semantic-analysis, diagnostic, fixture, and
lowering obligations. Implementation, test files, diagnostic-code allocation, and
commits are outside these documents.

Source of truth for the surface is
`../11-explicit-crossing-language-surface.md`. The approved design decisions,
diagnostic allocations, fixture inventories, and implementation-order constraints
are recorded in this directory and in `../NOTES.md`. No external agent memory is
required to reconstruct the Epic 11 contract.

## Block Index

| Block | Document | Surface | Status |
| --- | --- | --- | --- |
| 1 | `block-01-far-type-modifier.md` | `far T` type modifier | Implemented (gate enabled) |
| 2 | `block-02-on-placement-crossing.md` | `on dst { ... }` placement crossing | Implemented (gate enabled) |
| 3 | `block-03-spawn-on-remote-spawn.md` | `spawn on dst { ... }`, `far Task<T>` | Implemented (gate enabled) |
| 4 | `block-04-crossing-contracts.md` | inferred crossing effect, `@shard_movable`, `@shard_pinned` | Implemented (gate enabled) |

## Execution Scope

Epic 11 delivers the language surface (lexer, parser, sema) plus lowering guards
only. Accepted language forms are compile-only in the positive fixture matrix.
Execution remains guarded until the Phase 4 transport epic: lowering/backend
checks must report the deterministic backend-unavailable diagnostics documented
below instead of falling through to backend panics or runtime execution. See the
epic "Epic 11 Execution Scope" section.

## Forced Implementation Order

The blocks are individually documented but must be implemented in this order
because the golden fixtures cross-depend:

1. **Block 1 (`far` type modifier).** Establishes the `far T` type former. Blocks
   2, 3, and 4 all need `far` types to exist before their fixtures can even parse
   (`far Channel<T>` destinations, `far Task<T>` results, `far T` captures).
2. **Blocks 2 and 3 (`on` and `spawn on`).** Build the crossing forms on top of
   the Block 1 types. These may proceed in parallel with each other.
3. **Block 4 contracts.** Infer the crossing effect from `on`, `spawn on`,
   `far Task<T>` operations, and direct calls; enforce capture legality,
   shard-mobility validation, and shard-attribute conflict rules. This closes
   last because it judges the crossing forms introduced by Blocks 2 and 3.

The rule is: types first, then the crossing forms, then the semantic contract
that judges them.

## Shared Diagnostics Ownership

To keep one diagnostic per invariant, each crossing-effect and crossing-capture
diagnostic has a single owning block. Blocks 2 and 3 reference the owner's code
rather than allocating their own. New codes are reserved in
`internal/diag/codes_crossing.go` (a sibling of `codes.go`, kept separate only so
`codes.go` stays within its file-size ceiling); reuses point at existing codes in
`codes.go`. Reuse-first policy: reuse an existing code where the invariant already
exists; allocate new in the `SEM`/`SYN` range for new invariants; postponed
surfaces use the `FUT` 7xxx range.

| Code | Owner | Invariant | Referenced by |
| --- | --- | --- | --- |
| ~~`SEM3162`~~ | — | RETIRED — was "function crosses shards but not marked `crosses`". | none (code reserved) |
| ~~`SEM3163`~~ | — | RETIRED — was "non-`crosses` function calls a `crosses` function". | none (code reserved) |
| ~~`SEM3164`~~ | — | RETIRED — was "`far Task<T>.await()`/`.cancel()` outside a `crosses` function". | none (code reserved) |
| `SEM3165` | Block 4 | Borrowed value captured into a crossing boundary. | Block 2 (ON-CAP-N001/N002), Block 3 (C03, C04) |
| `SEM3166` | Block 4 | `@nosend` value crosses outside `@local spawn`. | Block 2 (ON-CAP-N003), Block 3 (C06) |
| `SEM3167` | Block 4 | `@shard_pinned` value crosses as `own T`. | Block 2 (ON-CAP-N004), Block 3 (C07) |
| `SEM3168` | Block 4 | Unmarked owned user value (incl. local `Task<T>`) crosses as `own T`. | Block 2 (ON-CAP-N005), Block 3 (C05, B07) |
| `SEM3169` | Block 4 | `@send`-only user type crosses as `own T`. | Block 3 (C08) |
| `SEM3170` | Block 4 | `@copy`-only user type crosses as `own T`. | Block 4 locality fixtures |
| `SEM3171` | Block 4 | `@shard_movable` type contains a non-shard-movable field/member. | Block 4 movable fixtures |
| `SEM3172` | Block 4 | `@shard_movable` conflicts with `@shard_pinned`. | Block 4 attr/pinned fixtures |
| `SEM3174` | Block 4 | `@local spawn on` is used. | Block 3 (S07) |
| `SEM3195` | Block 2 | `far Task<T>` used as an `on` destination. | Block 3 (T09) |
| `SEM3194` | Block 1 | Local operation on `far T` outside an accepted crossing context. | Block 2 (ON-ANCHOR-N003) |

> **`crosses` retirement note (design change D17, completed 2026-07-08).** The
> explicit `crosses` keyword has been REMOVED from the language: the crossing
> effect is retained but inferred at semantic analysis and stored in function
> metadata rather than required at a call/definition site. Consequences, now
> landed:
> - The `crosses` grammar is removed — `fn f() crosses -> T` no longer parses
>   (`SYN2012`/`SYN2205`). `Signature.Crosses` and its setter are removed.
> - `SEM3162`/`SEM3163`/`SEM3164` are RETIRED (numbers reserved, not reused).
>   `checkCrossesRequirement` and `checkCrossesCaller` are removed from sema, so
>   `on dst { }`, `spawn on`, and `far Task<T>.await()`/`.cancel()` are valid in
>   any function.
> - The crosses-requirement negatives are PARKED, not deleted, in
>   `testdata/golden/crossing/crosses_deferred/` for history. Active Block 4
>   fixtures now use valid source without a marker keyword, and
>   `internal/sema/crossing_effect_test.go` asserts the inferred `MayCross`
>   metadata directly.

Reused existing codes (per the infra map and reuse-first policy) include
`SemaUseAfterMove` (3130) for `far`-handle affinity and `far Task<T>`
double-await/await-after-cancel, `SemaTaskNotAwaited` (3107) for `far Task<T>`
drop-without-await, `SemaSpawnNotTask` (3111) for `spawn distributed { ... }`
(Block 3 S06), `SemaTypeMismatch` (3015) for `far`/non-`far` identity mismatches,
`SemaBorrowConflict` (3018) for `far`-handle borrow conflicts,
`SemaRawPointerNotAllowed` (3129) for `far *T` / `*far T`, and
`SynModifierNotAllowed` (2015) for `far` used as an item modifier.

## Historical Placeholder to Diagnostic-Code Mapping

Every `TBD-DIAG-*` placeholder that appeared in earlier block-matrix drafts has
been replaced in the docs with the allocated code below. New codes live in
`internal/diag/codes_crossing.go`; reuses point at existing `codes.go` codes.

| Placeholder | Code | Disposition |
| --- | --- | --- |
| `TBD-DIAG-FAR-RESERVED-IDENT` | `SYN2031` | new |
| `TBD-DIAG-FAR-NESTED` | `SEM3188` | new |
| `TBD-DIAG-FAR-REMOTE-OWN` | `SEM3189` | new |
| `TBD-DIAG-FAR-REMOTE-BORROW` | `SEM3190` | new |
| `TBD-DIAG-FAR-REMOTE-RAW-PTR` | `SEM3129` | reuse (existing) |
| `TBD-DIAG-FAR-RAW-PTR-HANDLE` | `SEM3129` | reuse (existing) |
| `TBD-DIAG-FAR-FN-HANDLE` | `FUT7011` | new |
| `TBD-DIAG-FAR-EXTERN-TARGET` | `SEM3191` | new |
| `TBD-DIAG-FAR-ITEM-MODIFIER` | `SYN2015` | reuse (existing) |
| `TBD-DIAG-FAR-GROUPING-UNSUPPORTED` | `SEM3192` | new |
| `TBD-DIAG-FAR-LOCAL-ARRAY-POSTPONED` | `FUT7010` | new |
| `TBD-DIAG-FAR-ARRAY-POSTPONED` | `FUT7009` | new |
| `TBD-DIAG-FAR-TYPE-MISMATCH` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-FAR-NONCAPABILITY` | `SEM3193` | new |
| `TBD-DIAG-FAR-HANDLE-MOVED` | `SEM3130` | reuse (existing) |
| `TBD-DIAG-FAR-HANDLE-NONCOPY` | `SEM3130` | reuse (existing) |
| `TBD-DIAG-FAR-HANDLE-BORROWED` | `SEM3018` | reuse (existing) |
| `TBD-DIAG-FAR-LOCAL-OP` | `SEM3194` | new |
| `TBD-DIAG-FAR-CONVERSION` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-ON-DST-FAR-TASK` | `SEM3195` | new |
| `TBD-DIAG-ON-DST-BLOCKING` | `FUT7012` | new |
| `TBD-DIAG-ON-DST-NOT-PLACEMENT` | `SEM3144` | new |
| `TBD-DIAG-ON-DST-TYPE-NAME` | `SEM3145` | new |
| `TBD-DIAG-ON-DST-BARE-FN` | `SEM3146` | new |
| `TBD-DIAG-ON-DST-SHARD-ID` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-ON-BODY-RETURN` | `SEM3147` | new |
| `TBD-DIAG-ON-BODY-MISSING-RET` | `SEM3148` | new |
| `TBD-DIAG-ON-RESULT-TASKRESULT` | `SEM3149` | new |
| `TBD-DIAG-ON-ANCHOR-UNPROVEN` | `SEM3150` | new |
| `TBD-DIAG-ON-ANCHOR-REQUIRED` | `SEM3194` | new |
| `TBD-DIAG-ON-TCP-REMOTE-IO` | `SEM3151` | new |
| `TBD-DIAG-ON-SUSPEND-CONTEXT` | `SEM3152` | new |
| `TBD-DIAG-ON-NESTED` | `SEM3153` | new |
| `TBD-DIAG-ON-BACKEND-UNAVAILABLE` | `FUT7014` | new |
| `TBD-DIAG-SPAWN-ON-DST-NOT-PLACEMENT` | `SEM3154` | new |
| `TBD-DIAG-SPAWN-ON-DST-TYPE-NAME` | `SEM3155` | new |
| `TBD-DIAG-SPAWN-ON-DST-BARE-FN` | `SEM3156` | new |
| `TBD-DIAG-SPAWN-ON-DST-FAR-HANDLE` | `SEM3157` | new |
| `TBD-DIAG-SPAWN-ON-DST-FAR-TASK` | `SEM3158` | new |
| `TBD-DIAG-SPAWN-ON-DST-BLOCKING` | `FUT7013` | new |
| `TBD-DIAG-SPAWN-ON-MISSING-BLOCK` | `SYN2032` | new |
| `TBD-DIAG-SPAWN-ON-MISSING-DST` | `SYN2033` | new |
| `TBD-DIAG-SPAWN-ON-BODY-RETURN` | `SEM3159` | new |
| `TBD-DIAG-SPAWN-ON-BODY-MISSING-RET` | `SEM3160` | new |
| `TBD-DIAG-SPAWN-ON-UNREACHABLE-AFTER-RET` | `SEM3161` | new |
| `TBD-DIAG-SPAWN-ON-LOCAL-TASK-ASSIGN` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-SPAWN-ON-RETURN-LOCAL-TASK-MISMATCH` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-FAR-TASK-AWAIT-RESULT-MISMATCH` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-FAR-TASK-CANCEL-RESULT-MISMATCH` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-SPAWN-ON-BACKEND-UNAVAILABLE` | `FUT7015` | new |
| `TBD-DIAG-FAR-TASK-AWAIT-BACKEND-UNAVAILABLE` | `FUT7016` | new |
| `TBD-DIAG-FAR-TASK-CANCEL-BACKEND-UNAVAILABLE` | `FUT7017` | new |
| `TBD-DIAG-CROSSES-MISSING` | `SEM3162` | retired/reserved |
| `TBD-DIAG-CROSSES-CALLER-MISSING` | `SEM3163` | retired/reserved |
| `TBD-DIAG-FAR-TASK-CROSSES-MISSING` | `SEM3164` | retired/reserved |
| `TBD-DIAG-CROSS-BORROW-CAPTURE` | `SEM3165` | new |
| `TBD-DIAG-CROSS-NOSEND-CAPTURE` | `SEM3166` | new |
| `TBD-DIAG-CROSS-PINNED-CAPTURE` | `SEM3167` | new |
| `TBD-DIAG-CROSS-NOT-SHARD-MOVABLE` | `SEM3168` | new |
| `TBD-DIAG-SHARD-MOVABLE-SEND-INSUFFICIENT` | `SEM3169` | new |
| `TBD-DIAG-SHARD-MOVABLE-COPY-INSUFFICIENT` | `SEM3170` | new |
| `TBD-DIAG-SHARD-MOVABLE-FIELD` | `SEM3171` | new |
| `TBD-DIAG-SHARD-ATTR-TARGET` | `SYN2016` | reuse (existing) |
| `TBD-DIAG-SHARD-ATTR-CONFLICT` | `SEM3172` | new |
| `TBD-DIAG-CROSSES-ATTRIBUTE` | `SEM3173` | new |
| `TBD-DIAG-CROSSES-PLACEMENT` | `SYN2034` | retired/reserved |
| `TBD-DIAG-CROSSES-TARGET` | `SYN2035` | retired/reserved |
| `TBD-DIAG-CROSSES-FN-TYPE` | `SYN2036` | retired/reserved |
| `TBD-DIAG-LOCAL-SPAWN-ON` | `SEM3174` | new |

## Golden Fixtures and Per-Block Test Gate

The Epic 11 golden fixtures already exist, gated so `make check` and
`make golden-check` stay green until each block is implemented.

**Location and naming.** Fixtures live under
`testdata/golden/crossing/block0{1..4}/{valid,invalid}/`. Unimplemented blocks
keep `_`-prefixed file names (e.g. `_on_negative_bad_anchor.sg`). The shell
golden runner (`scripts/golden_update.sh`) skips any basename starting with `_`,
so staged fixtures stay invisible to `make golden-check` until the block lands.
Once a block is implemented, its fixtures must drop the `_` prefix and commit the
generated sidecars (`.diag`, `.tokens`, `.ast`, `.fmt`) so `make golden-check`
becomes part of that block's proof. Positive fixtures are compile-only (parse +
sema, zero errors). Each negative fixture carries a `// EXPECT-DIAG: <CODE>`
header naming the exact diagnostic it must produce.

**Harness.** `internal/crossinggate` drives the fixtures under `go test`
(`make check`). It has four independent gate constants in
`internal/crossinggate/crossinggate.go`; Block 1 and Block 2 are enabled once
their surfaces land, and later blocks remain disabled until their implementation
slices are ready. When a gate is `true`, the harness runs each fixture at the
stage named by its optional `// EXPECT-STAGE:` header (default sema, via
`driver.Diagnose`): negatives must emit their `EXPECT-DIAG` code, positives must
be error-free.

**Backend-unavailable rows.** Fixtures whose expected codes are `FUT7014`,
`FUT7015`, `FUT7016`, or `FUT7017` prove lowering/backend guard behavior, not the
ordinary sema acceptance path. They intentionally mirror positive source shapes
under an unsupported execution configuration and must not be treated as plain
sema-negative fixtures while the matching positive compile-only fixtures are also
enabled. Block 2 resolves this with an explicit stage selector in the harness
(the pattern Block 3 must reuse for `FUT7015`–`FUT7017`):

- The fixture carries `// EXPECT-STAGE: backend` in addition to its
  `// EXPECT-DIAG:` header. `internal/crossinggate` routes such fixtures through
  `buildpipeline.Compile` with `BackendVM` and asserts the code on the resulting
  diagnostic bag, while every other fixture stays on the sema-stage
  `driver.Diagnose` path. The backend guard is unconditional, so no special
  backend/config value is needed.
- Because the diagnostic is emitted only at the backend stage, `surge diag`
  (sema) produces an empty `.diag`, which `scripts/golden_update.sh` rejects for
  `invalid/` fixtures. The backend-unavailable fixture therefore stays
  `_`-prefixed: `golden_update.sh` skips it (no committed sidecars, excluded from
  `make golden-check`), while the `crossinggate` go-test still exercises it
  because its `invalid/*.sg` glob matches `_`-prefixed names and the gate is the
  `BlockNEnabled` constant, not the prefix.

**Flipping a gate (one per block, in the forced order above).**

1. Implement the block's parser + sema.
   - For Block 4, the grammar slice lands before Blocks 2/3, while the semantic
     slice closes after Blocks 2/3. The single `Block4Enabled` constant is a
     final full-block gate; focused parser/sema unit tests carry the intermediate
     grammar-slice proof.
   - For backend-unavailable rows, set up the separate lowering/backend proof
     described above before enabling the affected full block gate.
2. Set the matching constant to `true` in
   `internal/crossinggate/crossinggate.go` (`Block1Enabled` … `Block4Enabled`).
3. Run `go test ./internal/crossinggate/` and fix implementation until the
   block's positives and negatives pass.

**Fold into the shell golden corpus.** Once a block passes under the gate, its
fixtures must join `make golden-check`: drop the `_` prefix from that block's
files, run `make golden-update`, commit the generated sidecars, and then run
`make golden-check`. The one exception is backend-unavailable fixtures
(`// EXPECT-STAGE: backend`), which stay `_`-prefixed and out of the shell corpus
(see "Backend-unavailable rows" above). Note that adding stdlib symbols (e.g. a
block's new intrinsics) shifts global `sym=`/`type#`/`L` IDs, so `make
golden-update` legitimately regenerates unrelated `hir`/`mir`/`mono` goldens with
renumbered IDs; that renumbering is expected and carries no structural change.
Until a block is implemented, the `crossinggate` harness is the authoritative
staged-fixture gate.
