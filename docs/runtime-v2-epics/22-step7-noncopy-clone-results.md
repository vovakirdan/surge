# D2 resolved non-Copy clone certificate

Verified 2026-10-09. Base: `a2c363b86fd7cb889e8f983e0ce8a3dfd7e26fcd`.
Source candidate: `c463722a04a3cc2beaa9dd619c19740bc4b3c177`.

## What changed

The return-origin analysis now consumes a finalized `DeferredCloneCall` whose
outcome selects one concrete, non-Copy `__clone(self: &T) -> T`. The selected
callable must still be the exact published symbol and body key recorded by the
closure, with one shared receiver, the same concrete receiver/result type, no
extra arguments and no generic callee arguments.

A selected source body is accepted only when its analyzed summary contains no
callable, unknown, expired or non-receiver source and no cell/backing poststate.
A body-less selection is accepted only for an intrinsic declaration whose
parameter effects and result borrowed-state classification are already proved.
Generic `__clone` implementations remain outside this certificate.

The original generic clone body keeps a conservative result: it reads the
cloned local binding's current contents, or preserves the source slot of an
exact direct shared-reference formal. It does not claim that a user clone is
deep or fresh when its body returns receiver-derived contents.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Source-body and intrinsic non-Copy clone cases PASS. A detached closure whose `CalleeKey` is changed is refused at the original clone site. Existing Copy, selected-clone, owner-publication and deferred-authority tests PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | Replacing only the resolved-outcome arm with the previous refusal restores all four non-Copy clone rows and the complete 51-row base result for `arrays_drop_nested`. The candidate has 13 rows. |
| Census | Exact clean-candidate sweep: 1187 programs, 44 unfinished, 0 timeouts. Exactly one program changes: `arrays_drop_nested` loses 38 rows, gains none and remains unfinished for 13 independent local reasons. No status, code or other reason set changes. |
| Repository check | `make check` remains red on existing return-origin-gated compiler/backend fixtures. The focused packages above are green; this packet does not claim the repository aggregate. |
| Structural | Sentrux remains at the existing red baseline: quality 0.53, coupling 0.08, one cycle. Both changed sema files pass the repository size check. |

## Limits

Only one corpus program carried the old non-Copy clone refusal:
`testdata/golden/vm_arrays/arrays_drop_nested.sg`. The certificate removes the
four direct rows in `core/array.sg` and 34 dependent unknown-source rows, but it
does not finish that program. Its remaining projected-payload, store-through-
place and unrelated callee-source rows belong to later D2 transfers.

The change does not alter clone selection, monomorphization, MIR, either
backend, or Runtime V2 scheduling and ownership. No VM/native acceptance is
claimed because the affected corpus witness remains fail-closed before code
generation.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-noncopy-clone/`:
candidate and counterfactual binaries, the exact targeted records and diff,
Sentrux output, and the resumable exact census.
