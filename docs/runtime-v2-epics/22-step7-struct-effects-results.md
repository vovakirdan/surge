# D2 borrowed-struct effect certificate

Verified 2026-10-09. Base: `0fa337783c5fb34072f7e79e820690163487bba0`.
Source candidate: `33a86fa776966c11572e0d5fae2aae04a7312e5b`.

## What changed

Calls receiving `&mut ViewParser` were treated as able to replace its
reference-bearing `BytesView` field, although the JSON validator mutates only
scalar cursor fields. This produced 68 mutable-effect rows per root and caused
same-statement literal borrows to be closed conservatively.

For a concrete source body over `&mut` plain struct, the analysis now identifies
reference/loan-bearing fields and proves them preserved. Assigning the whole
formal or a protected field fails. Passing a protected field is allowed only to
an exact shared formal; scalar fields are independent. Passing the whole struct
to another mutable source body recursively requires the same proof. Unknown,
generic, ambiguous and body-less calls fail closed; recursive validator cycles
are checked as one inductive component.

A shared temporary formal is confined only when the exact body does not return
its slot and every mutable borrow-holding formal has this field-preservation
proof. This keeps the three JSON literal borrows within their statements.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Recursive cursor readers/scalar writers are clean, including a same-statement string literal. A body assigning `p.data = other` retains the exact mutable-effect refusal. String-temporary, cell, backing and effect suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all 284 removed rows. |
| Census | Exact sweep: 1187 programs, 15 to 12 unfinished, 0 timeouts. The three standalone JSON roots each lose 71 rows and reach rc0. The JSON method golden loses the same 71 and remains on two independent imported tag-authority rows. Total 284 lost, 0 gained. Census SHA-256 is `397c36f6b8e7329f553c2f07d1511e2db1e042464c22e01b6dc85ca6c9aa7b3c`; comparison SHA-256 is `dcbccf2a965c53e20d8467b5b6498408258981f441c78aa06079bc9c0b763357`. |
| Binary identity | Candidate binary SHA-256 is `05809c8d54b25390cbb12ca5b99455134f30ff15dcb9e0d7bf44921f3626bcdf`; census identity records the candidate commit and an empty diff. |
| Backends | `testdata/llvm_smoke/json_validate.sg` exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final JSON validation binary is strict-Valgrind clean. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. |
| Structural | Sentrux reports quality 0.53 (5311), coupling 0.08, one cycle, broad complex-function count 1094 and the old cc=167 maximum. This packet costs one quality point and two broad-complex functions. |

## Limits

The certificate covers concrete source bodies and exact positional calls only.
It does not infer field aliases, accept generic/opaque callbacks, permit writes
to protected fields, or prove mutable effects through missing/ambiguous callees.
Named/default/variadic recursive calls remain outside this path.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-struct-effects/`:
candidate binary and identity, exact census/comparison, JSON VM/LLVM output,
strict Valgrind logs, focused canaries and Sentrux output.
