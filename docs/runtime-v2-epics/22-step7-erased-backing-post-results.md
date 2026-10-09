# D2 erased concrete backing-post certificate

Verified 2026-10-09. Base: `49261e7e46cc5ec9fcd465e55081fddd045c8c46`.
Source candidate: `210d7e663a6577c1efe3ffeae3be903de939aed9`.

## What changed

A generic mutator may keep `Unknown` in its template backing post-state because
`T` might retain a borrow or storage loan. `Array<T>::reverse_in_place` therefore
publishes `{Unknown, E(self)}` after its clone-and-store loop. At a concrete
`Array<Foo>` call, `Foo` contains owning strings but no borrowed state or loan;
the Unknown cannot inhabit any `Foo`, yet substitution reported `callee
returned an unproved source`.

Backing-post substitution now applies the existing `erasedType` predicate to
the concrete element. When that predicate proves the element can retain neither
a borrow nor a storage loan, the post is source-free. Loan carriers and all
reference-bearing concrete elements still take the ordinary substitution path.
The operation name and body spelling are irrelevant to the rule.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | A non-Copy plain record with a string field passes through generic `reverse_in_place` without Pending. Existing 59 backing-transfer rows and full `internal/sema` PASS. |
| Compiler packages | Focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores the one `list.reverse_in_place()` row and rc1 for `arrays_drop_nested`. |
| Census | Exact sweep: 1187 programs, 34 to 33 unfinished, 0 timeouts. Exactly `arrays_drop_nested` changes, loses its one row and moves to rc0. No row is gained and no diagnostic-code set changes. |
| Backends | The complete formerly blocked golden exits 0 on VM and LLVM; stdout is byte-identical with SHA256 `65840b50…582edaa2`. |
| Native ownership | **FAIL, retained:** strict Valgrind exits 99 with 36 B definitely lost in two 18 B string blocks. One stack reaches `rt_string_clone` through the specialized `reverse_in_place`; the other reaches `rt_string_from_bytes` directly from the main body. RV2-DEBT-467 owns the runtime residue. |
| Base controls | The preceding exact compiler already admits `Array<string>.reverse_in_place()` and loses 36 B in two 18 B blocks under strict Valgrind, proving the reverse/string clone class predates this certificate. A two-byte string-slice-to-writer control is strict-zero, so that simple slice path does not explain the second whole-golden record. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux reports quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1085 and the old cc=167 maximum. This adds one branch to an already bounded backing substitution; the count rises by one from 1084. |

## Limits

This rule applies only to a mutable backing post and only after concrete
substitution. It cannot erase a function result, a cell post, a reference-
bearing element or any type that `loanCarrier` recognizes. Missing post-state
facts remain an error. It does not prove the runtime's clone/drop balance.

The whole-golden Valgrind failure prevents a native ownership acceptance claim.
The VM/native output agreement shows behavior, while the saved base control
attributes only the reverse/string class. The second 18 B record remains
unattributed and is recorded as such rather than called pre-existing by
assumption.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-erased-post/`: final and
counterfactual compilers, exact census/comparison and identity, full VM/LLVM
outputs, failing strict-Valgrind log, the preceding-compiler reverse-string
control, the clean string-slice disproof, focused compiler results and Sentrux
output.
