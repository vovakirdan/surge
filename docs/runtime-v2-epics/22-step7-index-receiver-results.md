# D2 mutable indexed-receiver certificate

Verified 2026-10-09. Base census: canonical-core result at `ad9ccb76`.
Source candidate: `70ce4de05f25f1ebe1070b3f1f0e3f574898548b`.

## What changed

Sema already treats built-in Array/ArrayFixed indexing as a physical element
place and permits a mutable reborrow through it. Method-call lowering lost that
place: it lowered `xs[1]` as the shared `__index` result, then correctly refused
to upgrade the shared reference for `push(self: &mut Array<T>)`. MIR therefore
passed `copy L17`; LLVM happened to mutate through it while VM trapped with
VM2103.

Original generic-signature authority now accepts the mutable receiver only for
an exact typed scalar index over canonical Array/ArrayFixed with the selected
builtin body-less `__index`. HIR recognizes the same physical index and lowers
the receiver through `lowerPlaceExpr` plus `RefMut`, exactly as explicit
`&mut a[i]` does. Custom/shared index callables never enter this path.

## Verification

| Check | Result and limit |
| --- | --- |
| Source controls | `xs[1].push(9)` is clean. Pushing a fixed-array view through an indexed nested array still retains both the backing-loan-transfer and G6 loan-discard refusals. Alias/backing suites PASS. |
| Borrow boundary | `TestCustomIndexSharedCarrierRejectsMutableReborrowWithNote` remains green: custom shared `__index` is still refused. The VM store-through-shared-location guard is unchanged. |
| MIR | The target changes from `call push::<int>(copy L17, const 9)` to `call push::<int>(addr_of_mut L15[L17], const 9)`. |
| Backends | The exact target returns 14 on VM and LLVM with identical empty output. The focused e2e prints `3\n2\n` on both lanes. |
| Native ownership | The focused LLVM e2e is strict-Valgrind clean; the target runs under Valgrind with expected exit 14, empty stderr and no Memcheck error. |
| Compiler packages | Full `internal/sema` and `internal/hir` PASS; focused `internal/driver` PASS; relevant `go vet` PASS. |
| Census | Canonical 1187-record sweep moves 1 to 0 unfinished with 0 timeouts. Only `array_view_facts_a_resize_rule_withdraws.sg` changes, loses one row, gains none and moves rc1 to rc0. Census SHA-256 is `f9bf82b0402330561dbaeb63848f96346dd7da35609082ded79321804e2bb2e1`; comparison SHA-256 is `01bbe2eab163dc393941fe0c1559d5dc99e35fa5e2fb0b749606bcb903fee6b9`. |
| Binary identity | Candidate binary SHA-256 is `e751127c7f98e1688e7fb96c301e7280aa3e1674db079bd2a022d42e614d8bca`; identity SHA-256 is `4393d623947980b742d14c59876dd5ec123915c9a6d7642f407c1c82b5d80d89`. |
| Structural | Sentrux remains at quality 0.53 (5311), coupling 0.08, one cycle, broad complex-function count 1096 and the old cc=167 maximum. |

## Limits

This applies only to sema's physical built-in Array/ArrayFixed index place and
an exact `&mut self` method. It does not upgrade arbitrary shared references or
custom `__index` results and does not weaken borrow checking or VM location
mutability checks.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-final-index-receiver/`:
candidate binary and identity, canonical census/comparison, MIR, VM/LLVM output,
Valgrind logs and Sentrux output.
