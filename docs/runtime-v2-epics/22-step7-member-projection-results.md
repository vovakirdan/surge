# D2 composed member-projection certificate

Verified 2026-10-09. Base: `1edb94fa3091bfe9d6176d21eda69dbe87e68f90`.
Source candidate: `dde1a631fb0da4d2ca7b4d0ffed7650bf10df9c5`.

## What changed

The scalar array-index transfer already records the exact owner of the selected
element in both its value and storage facts. Member projection nevertheless
accepted only a syntactic identifier, so `entries[i].name` and a nested plain
field such as `s.note.text` discarded that owner and stopped at `projected
borrowed payload needs precise origin facts`.

The member certificate now composes through a preceding member or certified
scalar array index when its evaluated storage is a complete set of live local
or `V(param)` owners. The existing field fence is unchanged: the target must be
a reference to an unattributed plain struct and the selected field must still
be reference-free, with only the existing dynamic-array exception for storage
loans. An index is rechecked through its original declaration and concrete-use
certificate before its owner may be reused.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Direct, nested and indexed plain-field projections PASS. A composed projection whose inner reference is unproved keeps both the dereference and outer projection refusals. Attributed structs, fixed-array/cursor fields, borrowed-view contents and local-owner escapes retain their earlier controls. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` projection suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact pre-change compiler restores both probe projection refusals, both `stdlib/fs` rows and all seven direct projection rows in `arrays_drop_nested`, together with their two derived outgoing rows. |
| Census | Exact sweep: 1187 programs, 37 to 36 unfinished, 0 timeouts. Exactly two records change: `stdlib/fs/fs.sg` loses two rows and moves to rc0; `arrays_drop_nested` loses nine rows and remains unfinished on one call-source row plus three N-STORE rows. Eleven rows are lost, none gained; no diagnostic-code set changes. |
| Backends | The direct index-field and nested-field probe exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The LLVM probe is strict-Valgrind clean: exit 0 with empty stderr and no leaked or invalid access. |
| Broad check | The pre-commit `make check` remains red on the known return-origin-gated baseline fixtures, beginning with the direct stdlib JSON MIR-annotation test and continuing through existing backend/buildpipeline rows. No green broad-check claim is made. |
| Structural | Sentrux retains quality 0.53, coupling 0.08, one cycle, broad complex-function count 1084 and the old cc=167 violation. The production certificate file is 96 lines. |

## Limits

This closes composition of the existing plain-field certificate; it is not the
whole N-PROJ gap. Loading a reference through another reference, including an
element of `Array<&T>`, remains refused. `BytesView`, fixed-array and cursor
fields remain outside this certificate, as do maps, calls, choices, implicit
conversions, reference-bearing fields, nominal or attributed records, expired
owners and unresolved/captured owners. N-STORE is unchanged, which is why the
three nested stores in `arrays_drop_nested` remain.

No runtime or backend code changed. The Runtime V2 returned-source rule is
preserved: the projection reuses an already proved owner and adds no runtime
lifetime tracking. The VM/LLVM and Valgrind runs cover the newly admitted x86
probe only; they are not broader Runtime V2 or hardware acceptance.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-member-projection/`:
candidate and counterfactual compilers, exact census and comparison, the saved
census runner and identity, VM/LLVM outputs, strict Valgrind logs, focused
compiler results and Sentrux output.
