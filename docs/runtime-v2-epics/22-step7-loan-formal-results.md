# D2 source-body loan-formal certificate

Verified 2026-10-09. Base: `0a2676a6e057388172ea6188d08f2baf7cc15b3f`.
Source candidate: `8e9fdbbfd8a842e71e8d58114d8bab1c78b8c257`.

## What changed

G6 treated every reference-free by-value formal as a loan sink. That is correct
for opaque declarations, callbacks and ordinary payload-free values, but too
strong for a concrete source body whose formal type is itself a storage-loan
carrier. `sum_range(r: Range<int>) -> int` consumes the cursor in its body; the
three calls with array, view and fixed-array ranges were nevertheless refused
before the body transfer could preserve their owner.

A concrete, non-generic source body may now consume such a formal when its
result type cannot carry a storage loan. Opaque and body-less calls keep G6. A
source body returning a loan carrier also keeps G6: the full backing suite caught
`pass(v: uint64[]) -> uint64[]` as a laundering canary during validation, so the
final rule explicitly preserves that refusal.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | A source `consume(Range<int>) -> nothing` is clean. An opaque intrinsic with the same formal retains the exact loan-discard refusal. The existing pass-through array canary remains red, and the nested-constructor escape now reports exact SEM3139 for owner `xs`. |
| Compiler packages | The full typed backing suite PASS; full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all three `array_range_panics` loan-discard rows. |
| Census | Exact committed-tree sweep: 1187 programs, 24 to 23 unfinished, 0 timeouts. Only `testdata/golden/vm_intrinsics/array_range_panics.sg` changes, loses three rows and moves rc1 to rc0; no row or diagnostic-code set is gained. Census SHA-256 is `462aba6e78080f16f02d00516517394f4f5a4c9d5e0c4ee508760f021444f398`; comparison SHA-256 is `168c0c9d717374bf48470649a40f8d3e53f5034291f896deb6b8fcdeb928956e`. |
| Binary identity | Candidate binary SHA-256 is `8fc2e8c5673b8190d53901e8a02fd5b9a3336f511aeaad209a0ff79d53c42bf0`; census identity records the candidate commit and an empty diff. |
| Backends | The admitted golden exits 0 on VM and LLVM with identical stdout SHA-256 `d763548318d65c9b4d97383b4deb85b5a4c36b9ed82864cc5a8ef2bf29c25fed` and empty stderr. |
| Native ownership | The final LLVM binary exits 0 under strict Valgrind with identical stdout, empty stderr and no leak or invalid-access error. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1086 and the old cc=167 maximum, identical to this packet's base. |

## Limits

The exception requires the selected callee's concrete source body, a concrete
non-template formal whose type is a known loan carrier, and a result type which
cannot carry a loan. Callbacks, opaque declarations, body-less functions,
generic formals and pass-through result carriers remain guarded. This is a
type-bounded consumption certificate; it does not add a general alias model or
infer that an arbitrary body consumes a particular owner.

No runtime or backend code changed. VM and native already execute the range
cursor correctly; the analysis now allows the source-body call after retaining
the cursor's owner for the duration of the call.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-loan-formal/`: the exact
candidate binary and identity, census/comparison, preceding-compiler output,
VM/LLVM outputs, strict Valgrind logs, focused test logs and Sentrux output.
