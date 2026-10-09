# D2 generic crossing-reply certificate

Verified 2026-10-09. Base: `74b5f484312bc96f7864a3ce216ef642ff0a125a`.
Source candidate: `2728082b68f76e861dc050a6ac5072a53ba262f7`.

## What changed

An immediate `on` reply whose type still contained a function type parameter
was made Unknown in the generic body. Concrete uses therefore never got the
chance to prove that `TaskResult<int>` or `TaskResult<bool>` carries no borrowed
state.

A source generic body now records the existing `NoBorrowedState` condition on
that reply instead of emitting Unknown immediately. The condition is rebased at
each finalized use. Concrete inert payloads pass; a reference instantiation is
refuted at its call site. Non-generic and unsupported generic replies keep the
ordinary crossing refusal.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | `route<int>` and `route<bool>` are clean. `route<&int>` retains the exact concrete `NoBorrowedState` refusal. Existing on-crossing and generic-condition suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all eleven target rows. |
| Census | Exact committed-tree sweep: 1187 programs, 17 to 16 unfinished, 0 timeouts. Only `_integration_generic_crossing_sites.sg` changes, loses eleven rows, gains none and moves rc1 to rc0. Census SHA-256 is `e57a71251431e2e29eea74221a91af5afee95f9a408efade2118fa32d44e75a2`; comparison SHA-256 is `fe9d5a8c9625d1369bce645d0212524d7cb3c53c493728f4994d6ab2c54e408f`. |
| Binary identity | Candidate binary SHA-256 is `f09e0055f141d366309affa2b505bacfca14d1cab22f33482ccb24482d324d59`; census identity records the candidate commit and an empty diff. |
| Runtime limit | An executable-shaped probe stops at FUT7019 because immediate placement `on` requires an async context. No VM, LLVM, placement or ownership acceptance is claimed. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle and the old cc=167 maximum. Broad complex-function count rises from 1091 to 1092. |

## Limits

The conditional path requires a source generic body, a reply type containing
that body's parameters and the existing `NoBorrowedState` classifier. It does
not infer crossing ownership, admit captures, or bypass non-generic reply
checks. Runtime acceptance waits for an executable async placement path.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-generic-crossing/`:
candidate binary and identity, exact census/comparison, focused condition tests,
FUT7019 runtime attempts and Sentrux output.
