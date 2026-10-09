# Epic 22 Step 7 D2 closeout

Verified 2026-10-09. Final code candidate:
`70ce4de05f25f1ebe1070b3f1f0e3f574898548b`.

## Requested sequence

1. **SIGSEGV diagnosed.** Exact A/B on base `13873c62` and G5 found the same
   failure: both pass 10/10 at one carrier, fail 9/10 at eight carriers and pass
   on VM. GDB and TSan prove a pre-existing non-atomic heap-bigint refcount race
   between carrier threads. It is RV2-DEBT-452, not a G5 regression.
2. **G5 completed.** The counted local async capture packet is published and
   verified in `22-step7-g5-results.md`; its previous FAILs and limits remain
   explicit.
3. **SC-C then D2 completed in order.** SC-C made all ten mirrors byte-equal and
   retained its golden-gate limits. The subsequent D2 certificates reduce the
   exact source census to zero unfinished records.

## Final D2 evidence

The canonical sweep retains 1187 record keys and follows SC-C by diagnosing the
ten mirror keys through their absolute byte-equal `core/*.sg` sources. Final
result: **1187 programs, 0 unfinished, 0 timeouts**. The final transition is
one lost row, no gained rows and no diagnostic-code change.

This is source-analysis completion, not a claim that every repository gate or
runtime debt is green. `make check` retains its known baseline failures;
Sentrux remains below its historic baseline. Runtime/backend findings exposed
and retained during D2 include RV2-DEBT-452, 454, 467, 470, 474, 477 and 479.
FUT7008/FUT7014/FUT7019 limits recorded by the individual packets remain.

Evidence for every transition is in the adjacent `22-step7-*-results.md`
reports and under `~/.cache/surge-artifacts/step7-return-source/`.
