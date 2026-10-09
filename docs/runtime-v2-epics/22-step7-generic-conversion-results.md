# D2 exact generic source-conversion certificate

Verified 2026-10-09. Base: `5ee416d51e755fb1a3303a14d23c7f08d8452198`.
Source candidate: `7e43acd191c1d7a540e1fd6cd61c930685395e24`.

## What changed

A cast to `string` through generic `__to` publishes two legitimate concrete
uses at one source span: the selected conversion body and the compiler-created
`default<string>` target argument. Return-origin grouped finalized uses only by
caller and span, called those two different callees contradictory, and then
refused the cast because a generic source body had no operation-specific
certificate.

Finalized-use uniqueness now includes the callee template. The synthetic
`default<string>` is accepted only with its exact `conversion-target` witness.
A generic `__to` body is accepted only when the cast's selected symbol, concrete
instance, caller, source span and `call` witness all agree; the receiver is a
shared reference, every remaining effect is reference-free, the type marker
and result are the cast target, requirements pass, and the fixed-point summary
contains no source. Exact duplicate authority still fails.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Dynamic/fixed arrays of ints, arrays of a Printable user type, and an exact user generic source conversion finish. Existing selected-operator and body-conversion canaries stay red where the body is mutable, has unproved effects or returns a source. The duplicate-use corruption canary remains red. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all 16 removed rows. |
| Census | Exact committed-tree sweep: 1187 programs, 21 to 20 unfinished, 0 timeouts. `array_helpers.sg` loses all twelve rows and reaches rc0. Showcase 28 loses four conversion/authority rows but stays unfinished on independent generic-result and provenance rows. No row or diagnostic-code set is gained. Census SHA-256 is `57de4fa05159588eadbe6310d165379862a5315bc736997b4b7e29c2be34bcd6`; comparison SHA-256 is `eba8a1a530026172758e6b96af51502a73ae819b10077952b271ebc1930cdc1c`. |
| Binary identity | Candidate binary SHA-256 is `710e66525570f24f5252e3e4ae87d62da82d07de2fa82e6213cc2a0200a2116a`; census identity records the candidate commit and an empty diff. |
| Backends | A combined dynamic/fixed/Foo/user-generic conversion probe exits 0 on VM and LLVM with identical stdout SHA-256 `958fddde94f8d74228d0f747075811435c84294a919c53f5037cf11081428ec3` and empty stderr. |
| Native ownership | The array and Foo subsets are strict-Valgrind clean. The user-generic `Wrap<int>` subset loses one 17 B string and is retained as RV2-DEBT-477. The exact preceding compiler accepts the direct `w.__to("")` spelling and loses the identical 17 B with byte-identical output. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle and the old cc=167 maximum. Broad complex-function count rises from 1086 to 1088 for the two exact authority readers; this recorded increase remains secondary to the D2 census ruling. |

## Limits

The rule requires a selected synchronous source-body `__to` and the exact
finalized generic instance at the cast span. It does not accept opaque or
body-less conversions, mutable receivers, generic callers, ambiguous/duplicate
uses, unmatched target markers, reference-bearing effects, unmet requirements
or summaries which name an input, callable or unknown source.

Different callees may share one span because HIR synthesizes operations there;
two authorities for the same callee template at the same caller/span remain a
hard refusal. No runtime or backend code changed.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-generic-conversion/`:
candidate binary and identity, exact census/comparison, combined and split
VM/LLVM/Valgrind probes, preceding-compiler direct-call control and Sentrux
output.
