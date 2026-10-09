# D2 aggregate container-loan certificate

Verified 2026-10-09. Base: `559b94806978950326f8492ed9e232e43840459d`.
Source candidate: `a6e883174ff64adba16046c161fec7103423588e`.

## What changed

Member projection already preserved the owner of a loan-carrying field, but
`containerLoans` recognized only a local or formal whose own type was directly
Array/Range. In `Bag.__range(self: &Bag)`, `self.values` therefore had the exact
`self` owner while its array loan was rejected as having no proven base.

The loan read now uses the existing recursive `holdsLoan` predicate for a local
aggregate. For a shared-reference formal whose concrete referent recursively
holds a loan, it publishes `L(param)`. Legacy call substitution may rebase that
selector to the actual only for a non-container aggregate with the same proven
loan shape; direct container formals retain their backing-transfer path.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | `&Bag -> Range<int>` publishes source slot 0; in-frame iteration is clean. Returning the range from a local Bag built over a fixed-array view produces exact SEM3139 for the local holder and no Pending row. Nested-constructor, container-loan twin and typed backing suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all six aggregate-loan rows. |
| Census | Exact committed-tree sweep: 1187 programs, 20 to 19 unfinished, 0 timeouts. `magic_methods_repro.sg` loses three rows and reaches rc0; `imported_magic_methods.sg` loses the same three rows from its imported module and remains unfinished on four independent authority/index rows. No row or diagnostic-code set is gained. Census SHA-256 is `bebe208fbc55e72b51c920332daa8de71c526b211865a9d69342bebb536175c5`; comparison SHA-256 is `08aeafc0a91e181cb7bf47ca4e9205c4bb6d8ec4d26c729db91d158fc7ded978`. |
| Binary identity | Candidate binary SHA-256 is `5c484e834ad27e0c7eb3ca9b4fe21363b0e4e9b0391d46622e5377a3f12984d2`; census identity records the candidate commit and an empty diff. |
| Backends | A Bag/range iteration probe exits 0 on VM and LLVM with identical empty stdout/stderr. |
| Native ownership | **FAIL, retained:** the user `Bag.__range` probe loses one 40 B range object natively; RV2-DEBT-479 owns it. The exact preceding compiler's direct core `a.__range()` stored-range control is strict-Valgrind zero, so pre-existence is not claimed. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle and the old cc=167 maximum. Broad complex-function count rises from 1088 to 1090 for the aggregate-loan recognition/substitution branches; the increase is recorded rather than hidden. |

## Limits

The new selector applies only to a concrete non-container aggregate reached
through a shared-reference formal and proven recursively to store an existing
Array/Range loan. Canonical container formals, mutable post-state, unknown
generic shapes, pointers/functions and missing actuals keep their existing
backing or refusal paths. The call receives the aggregate's actual owner; this
does not create per-field alias identity.

No backend code changed. Source lifetime safety is proven by the SEM3139
negative row; RV2-DEBT-479 prevents treating the user-range path as native
ownership acceptance.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-aggregate-loan/`:
candidate binary and identity, exact census/comparison, VM/LLVM outputs, strict
Valgrind failure, direct-core-range control and Sentrux output.
