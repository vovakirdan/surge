# D2 module-qualified tag certificate

Verified 2026-10-09. Base: `de9e24a73bb67dd420eb449a2d32d6f7e9d70f90`.
Source candidate: `ad9ccb7648424f9bdf5cf36f9469cd5504736acd`.

## What changed

`json.JsonString("x")` selected the exact imported tag for both the call and
its member target, but tag transfer accepted only an identifier target. It then
evaluated `json.JsonString` as an unsupported module member and refused the
constructor.

A tag target may now be a module member only when the member target is an exact
module symbol, its field name equals the selected tag name, the member and call
map to the same selected `SymbolTag`, and the target has its typed descriptor.
All existing declaration-owner, signature, payload and finalized-use checks
still run unchanged.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Existing identifier tags, imported owners, tag conversions and corrupted authority controls PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores both target rows. |
| Census | Exact sweep: 1187 programs, 12 to 11 unfinished, 0 timeouts. Only `json_method_jsonvalue_param.sg` changes, loses two rows, gains none and reaches rc0. Census SHA-256 is `a52730a4c39ed68e3fdeb9eff5433c37bbc7694343ab8c4409ec6452d8f264e1`; comparison SHA-256 is `2721b8c41b6df0fbeb39fc4cb51a3257bde2ab457d12bcc49ab245d65401ae3e`. |
| Binary identity | Candidate binary SHA-256 is `9cb4212aab54afefcb80983f420a03894c1106f7f3fca6bb9c362c20859ae0f1`; census identity records the candidate commit and an empty diff. |
| Backends | The admitted golden exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final LLVM binary is strict-Valgrind clean. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. |
| Structural | Sentrux remains at quality 0.53 (5311), coupling 0.08, one cycle and the old cc=167 maximum. Broad complex-function count rises from 1094 to 1095. |

## Limits

This adds one syntactic declaration-target form. It does not infer a tag by
name, accept arbitrary module values, bypass owner uniqueness, or weaken generic
payload and conversion checks.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-module-tag/`: candidate
binary and identity, exact census/comparison, VM/LLVM output, strict Valgrind
logs and Sentrux output.
