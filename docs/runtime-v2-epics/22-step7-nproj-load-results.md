# D2 exact projected-load certificate

Verified 2026-10-09. Base: `e179715ebb1056e4e6d8088c878a29d9adce8e02`.
Source candidate: `f5b55eb5b85826b2ed9dcb9996b25a6a35d03d49`.

## What changed

Return-origin already tracked the contents of canonical array backings for
stores and `clone(xs[i])`. An explicit dereference of the same certified index,
`*xs[i]`, ignored those facts and treated the container owner as the element's
referent. The shared backing transfer is now used by clone and dereference, so
a reference element loads `E(slot)` or the exact owning local's stored origins.

The other admitted load is the exact core `BytesView`. Its declaration-only
borrowed-view marker already makes every holder keep the source string's loan.
A by-value read through `&BytesView` now retains that reference's source, and a
member projection may name a marked `BytesView` field's place. The scalar
`BytesView.__index` certificate then discards the evaluated view only after the
read is proven source-free.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | `*xs[0]` over a compiler-built variadic reference pack returns `E(0)`; returning a local reference through the same path gives SEM3139. Exact `BytesView` dereference and field reads finish; the field-reference summary retains slot 0. Existing backing, BytesView, member and field suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores the pack dereference refusal, the `BytesView` dereference and field refusals, and their derived probe rows. It also restores all 96 direct corpus rows. |
| Census | Exact sweep: 1187 programs, 36 unfinished before and after, 0 timeouts. Exactly the three standalone JSON roots and `json_method_jsonvalue_param` change; each loses 24 rows. Total: 96 rows lost, none gained, no rc or diagnostic-code set changes. The roots remain unfinished on 87/87/87/89 independent rows. |
| Backends | The pack-read plus `BytesView`-field probe exits 0 on VM and LLVM and prints `ab` on both. |
| Native ownership | The final LLVM probe is strict-Valgrind clean: exit 0, stdout `ab`, empty stderr, no leak or invalid access. |
| Broad check | The immediately preceding commit hook showed `make check` remains red on the known return-origin-gated compiler/backend fixtures. This packet does not claim that broad baseline is green. |
| Structural | Sentrux retains quality 0.53, coupling 0.08, one cycle, broad complex-function count 1084 and the old cc=167 violation. The new transfer file is 20 lines; the specialized BytesView, index-store and member files are 72, 225 and 101 lines. |

## Limits

This is a finite content model, not the deferred buffer-alias model `A(b)`.
The index path requires the existing canonical Array/ArrayFixed scalar-index
certificate and a backing target it can prove. Source-written arrays of
references remain SEM3138; the admitted language witness is the compiler-built
variadic reference pack. Views, slices, cursors, maps and arbitrary cells remain
outside this transfer.

Only the declaration that core marks as its borrowed view gets the `BytesView`
path. A look-alike cannot acquire that mark. Taking the `BytesView` itself out
of a shared reference remains SEM3197; the admitted reads consume it only as a
borrowed scalar-reader receiver. Fixed-array/cursor fields and reference-bearing
ordinary fields keep their prior member-projection fences.

No runtime or backend code changed. Runtime V2's returned-source contract is
preserved: array reads use the recorded backing contents and borrowed-view reads
retain their proven source; no runtime lifetime state is added.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-nproj-load/`:
final and counterfactual compilers, exact census and comparison, the saved
census runner and identity, focused compiler results, VM/LLVM outputs, strict
Valgrind logs and Sentrux output.
