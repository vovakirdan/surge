# D2 fixed-field projection certificate

Verified 2026-10-09. Base: `be8f0efe4b01c99eafb5d0e7887ce41f0ffe316d`.
Source candidate: `2bed912ec94fef6f2e028f9e888237d6445f4f0d`.

## What changed

Member projection kept a reference-free dynamic-array field's referent but
excluded fixed arrays and range cursors. That fence predated the checker work
which now keeps a call's `&` argument borrowed while a fixed-array window,
cursor or raw-pointer result lives. The dedicated checker canaries refuse both
reallocation of an array element containing the fixed field and overwrite of a
whole fixed-field record while the returned window is live.

A reference-free fixed-array or cursor field now uses the same field-place
rule as any other reference-free field: the projection names the referent's
sub-place and carries exactly that referent's origins. Later view/cursor
operations therefore publish the struct parameter as their return source, in
agreement with the borrow checker that holds it.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Fixed-field view and cursor summaries are clean and name slot 0. The existing grow-after-view and overwrite-after-view programs remain SEM3018/SEM3019 borrow-check refusals. Nested, attributed, unknown-owner and loan-content canaries retain their expected verdicts. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores the probe's field projection, result and derived call rows, and all 17 lost census rows. |
| Census | Exact sweep: 1187 programs, 36 to 35 unfinished, 0 timeouts. Exactly three records change: `self_mut_field_index_set` loses all eight rows and moves to rc0; `self_mut_field_reborrow` loses its projection row; the fixed-array operator golden loses eight rows. Seventeen rows are lost, none gained; no diagnostic-code set changes. |
| Backends | A returned window over a fixed struct field exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final LLVM probe is strict-Valgrind clean: exit 0, empty stdout/stderr, no leak or invalid access. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux retains quality 0.53, coupling 0.08, one cycle, broad complex-function count 1084 and the old cc=167 violation. `return_origin_member.go` shrinks to 99 lines. |

## Limits

The selected field must still have a fully known reference-free borrowed-content
shape, or be the exact marked `BytesView` handled by its separate transfer.
Ordinary reference-bearing fields, nominal/attributed records and unknown or
captured owners stay fail-closed. Loading the contents of a container field
still needs a proven backing; this packet certifies the field place and the
window/cursor source, not arbitrary contents.

`self_mut_field_reborrow` remains unfinished on three rows after the projection
clears. Its local `cells` is a double mutable reference to the fixed field, and
the first `cells[r]` step has no selected container transfer; the outer store
therefore remains unproved. No `A(b)` buffer alias or general referent graph is
introduced.

No runtime or backend code changed. The result agrees with Runtime V2's
returned-source rule and with the current checker/runtime behavior: the caller
keeps the containing record borrowed while the window points into its inline
field.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-fixed-field/`: final and
counterfactual compilers, exact census and comparison, the saved census runner
and identity, focused compiler/checker results, VM/LLVM outputs, strict
Valgrind logs and Sentrux output.
