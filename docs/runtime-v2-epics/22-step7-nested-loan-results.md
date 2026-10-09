# D2 nested constructor-loan certificate

Verified 2026-10-09. Base: `69caa547cd41183b1a9fe76cd8dc7444af986cd0`.
Source candidate: `8603fe771552694bf5f62b88fe7097a3ecbfdd44`.

## What changed

Constructor transfer classified a struct only by borrowed-content shape. A
record such as `ByteBuffer { data: byte[], start: uint }` is reference-free by
that measure, so the constructor discarded the loan carried by a dynamic-array
view in `data` and emitted `storage loan would be discarded by a payload-free
value`.

Constructors now also ask the existing recursive `holdsLoan(type)` predicate.
A reference-free aggregate which stores an array/cursor loan retains the joined
child origins. An aggregate with no nested loan keeps the old source-free path.
The retained owner then behaves normally: in-frame use is clean, and returning
a holder whose view points into a dying fixed array produces SEM3139 rather than
remaining Pending.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | A holder built from a fixed-array window is clean while used in frame. Returning the same holder names the fixed array owner with SEM3139 and leaves no Pending row. Existing constructor effects and the Duration holder canary PASS with the stronger diagnostic. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores the probe's one storage-loan-discard row and the exact prior `byte_lines` record. |
| Census | Exact sweep: 1187 programs, 25 to 24 unfinished, 0 timeouts. Exactly `benchmarks/native/byte_lines/main.sg` changes, loses its final row and moves from rc1 to rc0. No row is gained and no diagnostic-code set changes. |
| Backends | A holder/window probe exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final LLVM probe is strict-Valgrind clean: exit 0, empty stdout/stderr, no leak or invalid access. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux reports quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1086 and the old cc=167 maximum. The nested-loan branch raises the broad count by one from 1085; quality work remains secondary to the D2 census ruling. |

## Limits

This changes value propagation only for constructors whose concrete aggregate
type recursively reaches a supported storage-loan carrier. Unknown generic
shapes keep the existing constructed-result refusal. It does not create buffer
aliases, infer loans for arbitrary raw pointers, or weaken the loan-discard
guard at stores and calls.

No runtime or backend code changed. Native and VM already store the dynamic
array header inside the aggregate; the analysis now preserves the owner which
that header's view depends on.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-nested-loan/`: final and
counterfactual compilers, exact census/comparison and identity, focused
constructor/escape tests, VM/LLVM outputs, strict Valgrind logs and Sentrux
output.
