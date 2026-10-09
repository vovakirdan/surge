# D2 source-body operation-result certificate

Verified 2026-10-09. Base: `5fdac73091689c317284ea0c52921b9848d72758`.
Source candidate: `18977e3e6e7f1ec86a166aff3e83758a75348ff6`.

## What changed

Exact source-body operators and conversions were accepted only when their
fixed-point summary was source-free. `Arr.__add(&self, &other) -> uint64[]`
returns a fixed-field window, so its checked summary correctly named parameter
0; the operation path discarded that useful result and emitted an Unknown.

Body operations now receive the already evaluated operand value and storage
facts. A live summary root may transfer only `V(param)` to its corresponding
actual through the ordinary `callArgumentOrigin` rules. This preserves an
implicit shared borrow's storage owner. Expired roots, selectors other than
`V`, out-of-range slots, callables and erased results carrying roots stay
refused. Empty summaries keep the prior source-free/fresh-result certificate.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Borrowed `__add` through direct parameters and a reference binding publishes slot 0; a fresh `__mul` remains source-free. Existing operator, conversion and direct-clone suites PASS. Returning a local fixed-array window is still rejected earlier by the storage-view checker with SEM3198. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all twelve target rows. |
| Census | Exact committed-tree sweep: 1187 programs, 18 to 17 unfinished, 0 timeouts. Only `fixed_array_view_operator_stays_in_frame.sg` changes: twelve rows disappear, none appears, and it moves rc1 to rc0. Census SHA-256 is `8d0760b4d1c420f51012b2888fb58cc337073ee6dd867c4a092fa60a1c94369e`; comparison SHA-256 is `6c30d88f5eeed2983ad8c65fc3b8cefe133e8c6b56e0edf194b1e88cce9b5d06`. |
| Binary identity | Candidate binary SHA-256 is `4853367c38e93f4f25a900b7c1b1a0ba70c408e657da66df996da3c3f2b61aa9`; census identity records the candidate commit and an empty diff. |
| Backends | The admitted golden exits 0 on VM and LLVM with identical empty stdout/stderr. |
| Native ownership | The final LLVM binary is strict-Valgrind clean: exit 0, empty output and no leak or invalid-access error. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1091 and the old cc=167 maximum, unchanged from the packet base. |

## Limits

The selected operation, source body, generic instance, read-only effects and
requirements must already pass the existing body-operation certificate. Result
substitution supports only direct input-value roots. It does not transfer
container elements/loans, callable alternatives, post-state or unknown roots,
and it does not infer aliases between operands.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-operation-results/`:
candidate binary and identity, exact census/comparison, VM/LLVM output, strict
Valgrind logs and Sentrux output.
