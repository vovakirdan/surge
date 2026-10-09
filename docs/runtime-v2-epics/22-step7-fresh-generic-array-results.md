# D2 fresh generic-array helper certificate

Verified 2026-10-09. Base: `157567b51d4e67349aaef051f5f3c2800e6ae784`.
Source candidate: `8c089330257ed803fb51b21de49e629a36cca993`.

## What changed

Generic map/filter helpers construct a fresh local array, but callback results
and `clone(T)` made its contents Unknown. That Unknown was then mistaken for an
unknown array header/source even after concrete `int` uses proved the element
borrow-free.

An incoming callback is conditionally confined only when it is a declared
function parameter, has no captures, accepts shared inputs and its inputs/result
meet `NoBorrowedState`. A direct-template `clone(place)` accepts the exact local
or parameter place recorded by its deferred edge and adds the same condition.

A generic result may erase its single Unknown only when it returns the same
local binding initialized by an empty array literal, that binding is never
reassigned, and the concrete result element is reference-free and not a loan
carrier. A view/rebinding control retains source slot 0.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Concrete `mapping<int,int>` and `filter<int>` are clean. A helper which rebinds its initially empty result to `xs[[...]]` is not fresh and preserves slot 0. Callable, effect, clone, generic-result and loan-part suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all 127 removed rows. |
| Census | Exact sweep: 1187 programs, 16 to 15 unfinished, 0 timeouts. Showcase 28 loses all 17 rows and reaches rc0. Each of the ten core mirrors loses eleven derived array-helper rows but remains unfinished. Total: 127 lost, 0 gained, no diagnostic-code change. Census SHA-256 is `a0b16fb8fbed4a52f831869b5b3a50519d5794b0a77c762f21eb40a83aa78c7d`; comparison SHA-256 is `2adbf5a1f471aa88c4a170a821fb314104e4f468563007a122e589ac5e493b30`. |
| Binary identity | Candidate binary SHA-256 is `851b76020a5ae21282d6dd6950fb0c7fbe61072bf2e6257ff211b8db4668a372`; census identity records the candidate commit and an empty diff. |
| Backends | Showcase 28 exits 0 on VM and LLVM with identical stdout SHA-256 `01f87f7632630a2a1b341fac8694f5c8946424ae2d53fc6f856aa21e660d78a4` and empty stderr. |
| Native ownership | The final LLVM showcase binary is strict-Valgrind clean. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1092 and the old cc=167 maximum, unchanged from the packet base. |

## Limits

The certificate requires an exact generic source body, an empty-array local
returned unchanged, one conditional content Unknown, and a concrete
NoBorrowedState element. It does not accept callback captures, mutable callback
references, array-returning callbacks, result rebinding, views, nonempty
initializers, multiple returns, callable contents or loan-carrying elements.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-fresh-generic-array/`:
candidate binary and identity, exact census/comparison, VM/LLVM output, strict
Valgrind logs, focused canaries and Sentrux output.
