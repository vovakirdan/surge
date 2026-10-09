# D2 nested reference-free store certificate

Verified 2026-10-09. Base: `e9a24f02387f609dbc2ef67374a103c1f842f849`.
Source candidate: `106c64fb5071843f804dcd1b36c6f86a5c5a4d5b`.

## What changed

An exact core index-set normally updates the backing facts of the owning local
or mutable container formal. A nested place such as `grid[1][0]` or
`self.values[i]` names an inner container, so the outer owner does not match
that final container and the analysis fell through to `store through a place
needs reference-content transfer`.

When the final element and stored expression are reference-free and the element
is not a storage-loan carrier, the store cannot change any return-origin fact.
That case now finishes after the ordinary outer index-set certificate. Its
target must be a typed member without an implicit conversion, or a certified
index that yields a reference to the inner container. The RHS still passes
`discardLoans`, so a hidden local or parameter loan remains a named refusal.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Nested dynamic-array scalar store and scalar store through an array field finish. A nested store whose final element is `uint64[]` keeps the exact N-STORE refusal. Existing projection and place-store controls PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores both probe store rows and all six direct corpus rows. |
| Census | Exact sweep: 1187 programs, 36 unfinished before and after, 0 timeouts. Exactly four records change: `arrays_drop_nested` loses its three N-STORE rows; `self_mut_field_index_set` loses one; `magic_methods_repro` and its imported root lose one each. Six rows are lost, none gained; no rc or diagnostic-code set changes. |
| Backends | The nested-grid and member-field store probe exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final LLVM probe is strict-Valgrind clean: exit 0, empty stdout/stderr, no leak or invalid access. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated compiler/backend baseline. This packet does not claim it is green. |
| Structural | Sentrux retains quality 0.53, coupling 0.08, one cycle, broad complex-function count 1084 and the old cc=167 violation. The new production certificate is 47 lines; `return_origin_index_store.go` is 228 lines. |

## Limits

This is the no-content branch of N-STORE. A reference-bearing or loan-carrying
final element still needs backing or alias facts and stays refused. Views,
slices and cursors do not acquire the deferred `A(b)` buffer-alias model.
Implicit conversions and non-index/member targets remain outside the
certificate.

At this commit `self_mut_field_reborrow` retained its N-STORE row because
`cells[r]` first read through a double mutable reference. RV2-DEBT-465 later
closes the exact local field-reborrow initializer; an arbitrary double-reference
parameter stays refused. That was a projected-place/reference-load gap, not
evidence that this no-content store transfer changed a backing.

No runtime or backend code changed. Runtime V2's ownership model is unaffected:
the accepted store writes only data that cannot carry a borrow or storage loan,
so no lifetime fact is created, replaced or hidden.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-nstore/`: final and
counterfactual compilers, exact census and comparison, the saved census runner
and identity, focused compiler results, VM/LLVM outputs, strict Valgrind logs,
the loan-carrier canary and Sentrux output.
