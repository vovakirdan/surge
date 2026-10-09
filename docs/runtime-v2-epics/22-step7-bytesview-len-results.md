# D2 deferred BytesView length certificate

Verified 2026-10-09. Base: `93728de896df0d07cad6b50b4045fa3c210b7bb2`.
Source candidate: `3ef4916c618138d2c760e8407a17167eb281a3bf`.

## What changed

The generic core `len<T: HasLength<T>>` already resolves `self.__len()` through
the finalized deferred-method authority. Its `BytesView` instance nevertheless
stopped at the general effect fence because the shared receiver contains raw
pointers to borrowed string bytes.

Deferred-method validation now reuses the existing
`returnOriginBytesViewReader` certificate before issuing that refusal. The
certificate names the retained core intrinsic declaration, not the method
spelling: it requires a non-generic body-less `__len(self: &BytesView) -> uint`,
the marked core borrowed-view type, its exact owner/ptr/len fields and original
publication. The ordinary loan-sink check still runs after the exemption.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | New `len(&view)` deferred instance PASS; the full deferred-method, generic-implementation, BytesView and authority rows PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | Removing only the existing BytesView-reader exemption restores the sole `core/base.sg:2225-2237` effect row and `abi_string_bytesview` returns to rc1. |
| Census | Exact clean-candidate sweep: 1187 programs, 43 unfinished, 0 timeouts. Exactly `abi_string_bytesview` moves from its one unfinished row to rc0; no row is gained and no other status, code or reason set changes. |
| Golden generator | Known errors fall from 19 to 18. Exactly `abi/abi_string_bytesview.sg` disappears and no new error appears; strict golden remains RV2-DEBT-421 FAIL. |
| Backends | VM and LLVM both exit 0 and print the exact golden output: `161`, `162`, `195`, `169`. |
| Native ownership | The LLVM binary produces the same output under strict Valgrind; exit 0, empty Valgrind stderr, no definite/indirect leak or invalid access. |
| Structural | Sentrux keeps the existing quality 0.53, coupling 0.08, one cycle and old max-cc violation. Its broad complex-function count rises from 1081 to 1082; the changed sema file is 166 lines and passes the repository size gate. |

## Limits

This is not a general promise that a body-less deferred method cannot mutate
reference-bearing state. User methods, function values, other borrowed views
and other intrinsic names retain the general effect refusal. The certificate
does not change BytesView ownership: the view still borrows its source string,
and existing escape/store checks remain mandatory.

The runtime model, BytesView layout, lowering and both backends are unchanged.
The run proves only the admitted ABI golden on this x86 host; it is not a broad
Runtime V2 or hardware acceptance result.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-bytesview-len/`:
candidate/counterfactual compilers, the targeted records, VM/LLVM outputs,
strict Valgrind logs, Sentrux output and the resumable exact census.
