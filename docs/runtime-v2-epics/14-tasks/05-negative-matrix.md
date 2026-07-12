# Epic 14 Task 5: Negative Matrix, Payload Negatives, Fallback Audit

**Status:** complete (2026-07-12). All four scopes landed as tests only
(no production code). Closeout Sentrux (committed tree): identical to the
Task 4 closeout — root `6183` (RV2-DEBT-029 unchanged, equality `0.4486`),
`internal` `6507`, `runtime` `5310`, `runtime/native` `5399`.

One boundary sharpened while writing rows: a heap-owning capture (e.g. a
`string`) into an anchored body is rejected by SEMA (SEM3168, not
shard-movable) before any backend gate — a stronger wall than the payload
guard; the guard-level negative is the `@shard_movable` capture, which
passes sema and stops at the executable gate (FUT7014). Task 5b names
these causes in the message text.
**Kind:** tests + audit (no production code expected).
**Depends on:** Task 4 (both channel forms open on LLVM).

## Scope

With `channel_on` and `on ch` open on LLVM, every rejection boundary must
be pinned by a test so the flip cannot silently widen:

1. **Backend negative matrix.** The anchored executable shape joins the
   VM/unknown guarded matrix (FUT7014 alongside the placement forms);
   `channel_on` keeps FUT7018 off LLVM (already pinned by
   `TestChannelOnGuardMatrix`); imported-module coverage already carries
   an `on ch` form (`TestCrossingBackendGuardsCoverImportedModules`).
2. **Compile-time payload negatives** (`TestLLVMTransportPayloadGuard`
   rows): a heap-element mint (`channel_on::<string>`), the union reply
   (`ret ch.recv()` — would ship an owner-heap pointer), a captured
   `far Task` lease inside an anchored body, and a heap-owning capture
   crossing into an anchored body.
3. **Sema negatives for the anchored context**: suspension in a blocking
   context (SEM3152) and a borrowed capture (SEM3165) with a far-handle
   destination — the placement variants exist, the far-handle rows pin
   that the same walls hold behind the new destination kind.
4. **Hidden-fallback audit extension**: the `unsupported_fallback_attempts`
   tripwire is asserted by the immediate-on and anchored round-trip rows;
   the far-channel create row gains the same assertion so every transport
   form watches the canary.

## Gates

`make check`, buildpipeline + sema suites, behavior suite for the harness
row, committed-tree Sentrux comparison at closeout. Baselines (Task 4
closeout): root `6183`, `internal` `6507`, `runtime` `5310`,
`runtime/native` `5399`.
