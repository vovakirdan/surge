# Epic 11 Task Blocks

This directory holds the documentary test-case matrices and fixture inventories
for Epic 11 (explicit crossing language surface). Each block document is
documentary only: it records parser, semantic-analysis, diagnostic, fixture, and
lowering obligations. Implementation, test files, diagnostic-code allocation, and
commits are outside these documents.

Source of truth for the surface is
`../11-explicit-crossing-language-surface.md`. The approved design decisions for
this prep pass are recorded in ruflo memory
(`surge-runtime-v2/epic11-prep-decisions`) and the compiler infrastructure map in
`surge-runtime-v2/epic11-infra-map`.

## Block Index

| Block | Document | Surface | Status |
| --- | --- | --- | --- |
| 1 | `block-01-far-type-modifier.md` | `far T` type modifier | Matrix drafted |
| 2 | `block-02-on-placement-crossing.md` | `on dst { ... }` placement crossing | Matrix drafted |
| 3 | `block-03-spawn-on-remote-spawn.md` | `spawn on dst { ... }`, `far Task<T>` | Matrix drafted |
| 4 | `block-04-crossing-contracts.md` | `crosses`, `@shard_movable`, `@shard_pinned` | Matrix drafted |

## Execution Scope

Epic 11 delivers the language surface (lexer, parser, sema) plus lowering guards
only. Every crossing execution path emits a deterministic backend-unavailable
diagnostic until the Phase 4 transport epic, and all positive golden fixtures are
compile-only. See the epic "Epic 11 Execution Scope" section.

## Forced Implementation Order

The blocks are individually documented but must be implemented in this order
because the golden fixtures cross-depend:

1. **Block 1 (`far` type modifier).** Establishes the `far T` type former. Blocks
   2, 3, and 4 all need `far` types to exist before their fixtures can even parse
   (`far Channel<T>` destinations, `far Task<T>` results, `far T` captures).
2. **Block 4 grammar slice.** Land the `crosses` parsing (contextual keyword in
   signature position) and the `@shard_movable` / `@shard_pinned` attribute
   parsing. Blocks 2 and 3 fixtures are written inside `crosses` functions and
   capture attribute-marked values, so this grammar must parse first.
3. **Blocks 2 and 3 (`on` and `spawn on`).** Build the crossing forms on top of
   the Block 1 types and the Block 4 grammar. These may proceed in parallel with
   each other.
4. **Block 4 sema slice.** Enforce `crosses` propagation, capture legality,
   shard-mobility validation, and the conflict rules. This closes last because it
   checks the crossing forms introduced by Blocks 2 and 3, so those forms must
   exist before the semantic checks can be exercised.

The rule is: types first, then the grammar that Blocks 2/3 are written in, then
the crossing forms, then the semantic contract that judges them.

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
| `SEM3162` | Block 4 | Function performs crossing work but is not marked `crosses`. | Block 2 (ON-CROSS-N001), Block 3 (X03) |
| `SEM3163` | Block 4 | Non-`crosses` function calls a `crosses` function. | Block 3 (X04) |
| `SEM3164` | Block 4 | `far Task<T>.await()` / `.cancel()` used outside a `crosses` function. | Block 3 (T07, T08) |
| `SEM3165` | Block 4 | Borrowed value captured into a crossing boundary. | Block 2 (ON-CAP-N001/N002), Block 3 (C03, C04) |
| `SEM3166` | Block 4 | `@nosend` value crosses outside `@local spawn`. | Block 2 (ON-CAP-N003), Block 3 (C06) |
| `SEM3167` | Block 4 | `@shard_pinned` value crosses as `own T`. | Block 2 (ON-CAP-N004), Block 3 (C07) |
| `SEM3168` | Block 4 | Unmarked owned user value (incl. local `Task<T>`) crosses as `own T`. | Block 2 (ON-CAP-N005), Block 3 (C05, B07) |
| `SEM3169` | Block 4 | `@send`-only user type crosses as `own T`. | Block 3 (C08) |
| `SEM3174` | Block 4 | `@local spawn on` is used. | Block 3 (S07) |
| `SEM3143` | Block 2 | `far Task<T>` used as an `on` destination. | Block 3 (T09) |
| `SEM3142` | Block 1 | Local operation on `far T` outside an accepted crossing context. | Block 2 (ON-ANCHOR-N003) |

Reused existing codes (per the infra map and reuse-first policy) include
`SemaUseAfterMove` (3130) for `far`-handle affinity and `far Task<T>`
double-await/await-after-cancel, `SemaTaskNotAwaited` (3107) for `far Task<T>`
drop-without-await, `SemaSpawnNotTask` (3111) for `spawn distributed { ... }`
(Block 3 S06), `SemaTypeMismatch` (3015) for `far`/non-`far` identity mismatches,
`SemaBorrowConflict` (3018) for `far`-handle borrow conflicts,
`SemaRawPointerNotAllowed` (3129) for `far *T` / `*far T`, and
`SynModifierNotAllowed` (2015) for `far` used as an item modifier.

## Placeholder to Diagnostic-Code Mapping

Every `TBD-DIAG-*` placeholder that appeared in the block matrices has been
replaced in the docs with the allocated code below. New codes live in
`internal/diag/codes_crossing.go`; reuses point at existing `codes.go` codes.

| Placeholder | Code | Disposition |
| --- | --- | --- |
| `TBD-DIAG-FAR-RESERVED-IDENT` | `SYN2031` | new |
| `TBD-DIAG-FAR-NESTED` | `SEM3136` | new |
| `TBD-DIAG-FAR-REMOTE-OWN` | `SEM3137` | new |
| `TBD-DIAG-FAR-REMOTE-BORROW` | `SEM3138` | new |
| `TBD-DIAG-FAR-REMOTE-RAW-PTR` | `SEM3129` | reuse (existing) |
| `TBD-DIAG-FAR-RAW-PTR-HANDLE` | `SEM3129` | reuse (existing) |
| `TBD-DIAG-FAR-FN-HANDLE` | `FUT7011` | new |
| `TBD-DIAG-FAR-EXTERN-TARGET` | `SEM3139` | new |
| `TBD-DIAG-FAR-ITEM-MODIFIER` | `SYN2015` | reuse (existing) |
| `TBD-DIAG-FAR-GROUPING-UNSUPPORTED` | `SEM3140` | new |
| `TBD-DIAG-FAR-LOCAL-ARRAY-POSTPONED` | `FUT7010` | new |
| `TBD-DIAG-FAR-ARRAY-POSTPONED` | `FUT7009` | new |
| `TBD-DIAG-FAR-TYPE-MISMATCH` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-FAR-NONCAPABILITY` | `SEM3141` | new |
| `TBD-DIAG-FAR-HANDLE-MOVED` | `SEM3130` | reuse (existing) |
| `TBD-DIAG-FAR-HANDLE-NONCOPY` | `SEM3130` | reuse (existing) |
| `TBD-DIAG-FAR-HANDLE-BORROWED` | `SEM3018` | reuse (existing) |
| `TBD-DIAG-FAR-LOCAL-OP` | `SEM3142` | new |
| `TBD-DIAG-FAR-CONVERSION` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-ON-DST-FAR-TASK` | `SEM3143` | new |
| `TBD-DIAG-ON-DST-BLOCKING` | `FUT7012` | new |
| `TBD-DIAG-ON-DST-NOT-PLACEMENT` | `SEM3144` | new |
| `TBD-DIAG-ON-DST-TYPE-NAME` | `SEM3145` | new |
| `TBD-DIAG-ON-DST-BARE-FN` | `SEM3146` | new |
| `TBD-DIAG-ON-DST-SHARD-ID` | `SEM3015` | reuse (existing) |
| `TBD-DIAG-ON-BODY-RETURN` | `SEM3147` | new |
| `TBD-DIAG-ON-BODY-MISSING-RET` | `SEM3148` | new |
| `TBD-DIAG-ON-RESULT-TASKRESULT` | `SEM3149` | new |
| `TBD-DIAG-ON-ANCHOR-UNPROVEN` | `SEM3150` | new |
| `TBD-DIAG-ON-ANCHOR-REQUIRED` | `SEM3142` | new |
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
| `TBD-DIAG-CROSSES-MISSING` | `SEM3162` | new |
| `TBD-DIAG-CROSSES-CALLER-MISSING` | `SEM3163` | new |
| `TBD-DIAG-FAR-TASK-CROSSES-MISSING` | `SEM3164` | new |
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
| `TBD-DIAG-CROSSES-PLACEMENT` | `SYN2034` | new |
| `TBD-DIAG-CROSSES-TARGET` | `SYN2035` | new |
| `TBD-DIAG-CROSSES-FN-TYPE` | `SYN2036` | new |
| `TBD-DIAG-LOCAL-SPAWN-ON` | `SEM3174` | new |

## Golden Fixtures and Per-Block Test Gate

The Epic 11 golden fixtures already exist, gated so `make check` and
`make golden-check` stay green until each block is implemented.

**Location and naming.** Fixtures live under
`testdata/golden/crossing/block0{1..4}/{valid,invalid}/`. Every file name is
`_`-prefixed (e.g. `_far_negative_nested.sg`). The shell golden runner
(`scripts/golden_update.sh`) skips any basename starting with `_`, so these
fixtures are invisible to `make golden-check` and never get a (wrong) `.diag`
generated while the surface is unimplemented. Positive fixtures are compile-only
(parse + sema, zero errors). Each negative fixture carries a
`// EXPECT-DIAG: <CODE>` header naming the exact diagnostic it must produce.

**Harness.** `internal/crossinggate` drives the fixtures under `go test`
(`make check`). It has four independent gate constants in
`internal/crossinggate/crossinggate.go`, all `false` today, so every block's test
`t.Skip`s cleanly. When a gate is `true`, the harness runs each fixture through
`driver.Diagnose` at the sema stage: negatives must emit their `EXPECT-DIAG`
code, positives must be error-free.

**Flipping a gate (one per block, in the forced order above).**

1. Implement the block's parser + sema.
2. Set the matching constant to `true` in
   `internal/crossinggate/crossinggate.go` (`Block1Enabled` … `Block4Enabled`).
3. Run `go test ./internal/crossinggate/` and fix implementation until the
   block's positives and negatives pass.

**Optional: fold into the shell golden corpus.** Once a block passes under the
gate, its fixtures can additionally join `make golden-check`: drop the `_`
prefix from that block's files and run `make golden-update` to generate the
committed sidecars (`.diag` etc.). Until then the `crossinggate` harness is the
authoritative gate.
