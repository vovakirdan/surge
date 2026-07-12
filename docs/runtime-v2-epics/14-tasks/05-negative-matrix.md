# Epic 14 Task 5: Negative Matrix, Payload Negatives, Fallback Audit

**Status:** in progress (2026-07-12).
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
