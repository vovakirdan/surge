# D2 imported magic-operation certificate

Verified 2026-10-09. Base: `44574048ba989b332c28a74eb929f8ddb8e5833e`.
Source candidate: `030227d0afabdd9aea9008ea8e127fd7dd8d932d`.

## What changed

Two concrete imported magic operations reached lowering with enough type
authority but without the root-file shape return-origin expected.

First, direct `clone(x)` selected a non-generic user `__clone` source body, but
the direct path accepted only Copy or body-less intrinsic clones. It now reuses
the exact source-body operation certificate: shared/read-only effects,
satisfied requirements and a source-free fixed-point summary.

Second, an imported non-generic `bag[index]` reached HIR without a root
`IndexSymbols` entry. Only in that absence, return-origin searches the indexed
program for one off-unit source body whose existing `indexCallAgrees` proof
matches the exact receiver, index and result types. Zero or multiple matches
remain refused; ordinary published selections keep their old path.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Direct source-body clone joins the already clean intrinsic/Copy rows; absent, invalid and wrong selections stay red. Exact local index-call controls, generic/mutable/reference/loan/effect canaries and the unique imported recovery path PASS. The earlier `Range` consumer expectation was updated to the source-body loan-formal rule already closed by RV2-DEBT-472. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all four imported-root rows. |
| Census | Exact committed-tree sweep: 1187 programs, 19 to 18 unfinished, 0 timeouts. Only `imported_magic_methods.sg` changes: four rows disappear, none appears, and the golden moves rc1 to rc0. Census SHA-256 is `c6540b0d392ded426795c67804534492906c1286bf585eaee2fd71dadc891316`; comparison SHA-256 is `a2896fb60c21fba91df32ce5d90a27d0fd885ebb3ff449541b4e572c3bd99da6`. |
| Binary identity | Candidate binary SHA-256 is `e8d9f31bb5de7ab5f819a72d2274484fbc448a90cd93e8659033194790ba5d78`; census identity records the candidate commit and an empty diff. |
| Backends | The admitted imported golden exits 0 on VM and LLVM with identical empty stdout/stderr. |
| Native ownership | **FAIL, already owned:** strict Valgrind reports two 40 B range objects, one for each user `Bag.__range` loop. This is exactly RV2-DEBT-479's class exposed by the preceding aggregate-loan packet; no clone/index loss appears. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle and the old cc=167 maximum. Broad complex-function count rises from 1090 to 1091 for the imported-index recovery branch. |

## Limits

Source clone admission requires the already selected concrete `__clone` body
and the same result/effect/summary proof as other body operations. It does not
turn the `clone` identifier into a callable value.

Index recovery is used only when the root has no `IndexSymbols` entry. The body
must live in another indexed unit and be the unique exact match; generic,
mutable, reference/loan result, unproved-effect, ambiguous and namesake methods
remain refused. This does not synthesize or publish a missing symbol table row.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-imported-magic/`:
candidate binary and identity, exact census/comparison, VM/LLVM output, strict
Valgrind logs and Sentrux output.
