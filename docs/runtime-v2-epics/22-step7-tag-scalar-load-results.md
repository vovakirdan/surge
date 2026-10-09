# D2 Copy tag-payload load certificate

Verified 2026-10-09. Base: `1a2c8c8bd8fa7c79127a880a39d05c0beedfdf18`.
Source candidate: `423d61234b2b4c9f11ff498df5beaaec57e27bc3`.

## What changed

Sema types an indexed element as a shared reference. When that expression is
passed to a by-value parameter, HIR's `applyParamBorrow` reads through the
reference unless sema recorded the argument in `ReferenceValueArgs` because the
instantiated value itself is a reference. Tag return-origin validation compared
the pre-HIR argument type directly with the payload and therefore rejected
`Some(xs[idx])` even when the payload was a Copy scalar.

The tag certificate now consumes that existing typed distinction. A mismatched
argument is accepted only when it is a shared reference to the exact payload,
the argument is absent from `ReferenceValueArgs`, and the payload is Copy,
reference-free and carries no storage loan. The exact tag owner, signature,
generic instantiation and payload-slot checks still run unchanged.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | `Some(xs[idx])` finishes with a source-free result. `Some::<&int>(r)` is pinned as reference-as-value and retains source slot 0. Existing template-tag, instantiated-caller, generic-result-loan and 59 backing rows PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | Restoring only the exact-type payload check restores all six `option_pipeline` rows. |
| Census | Exact clean-candidate sweep: 1187 programs, 38 unfinished, 0 timeouts. Exactly `showcases/24_option_pipeline/main.sg` loses its six rows and moves to rc0; no row is gained and no other status, code or reason set changes. |
| Backends | VM and LLVM both exit 0 and print the same output: `v = 20`, `w = 0`, `n = 42`. |
| Native ownership | The LLVM binary prints the same output under strict Valgrind; exit 0, empty Valgrind stderr, no definite/indirect leak or invalid access. |
| Structural | Sentrux keeps quality 0.53, coupling 0.08, one cycle, broad complex-function count 1083 and the old max-cc violation. `return_origin_tags.go` is 252 lines and passes the size gate. |

## Limits

This is not a general reference-to-value conversion. Mutable references,
non-Copy payloads, reference-bearing/loan-carrying payloads and every argument
marked `ReferenceValueArgs` remain outside the certificate. Ordinary call
semantics and tag ownership authority are unchanged.

No runtime or backend code changes. The runtime evidence covers the admitted
showcase on this x86 host and does not imply broad Runtime V2 acceptance.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-tag-scalar-load/`:
candidate/counterfactual compilers, exact record, VM/LLVM outputs, strict
Valgrind logs, Sentrux output and the resumable exact census.
