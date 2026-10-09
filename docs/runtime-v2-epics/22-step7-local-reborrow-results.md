# D2 local field-reborrow index certificate

Verified 2026-10-09. Base: `db1dfb127f3e34f2388b29368d25b8d24ed8b68a`.
Source candidate: `35f8c1b491c8f1754791c4b5f40cf964760703bf`.

## What changed

`let cells = &mut self.cells` records two mutable reference layers: the member
is already a mutable reference to the field, and the explicit reborrow wraps
it once more. Index transfer intentionally peels only one reference because an
arbitrary `&mut &mut ArrayFixed` parameter names an external reference cell,
not the container owner. The local reborrow therefore stopped before the
already certified nested reference-free store.

The new certificate recognizes only a same-function `let` initialized by an
exact `&mut` of one struct field. It rechecks the local symbol and statement,
both mutable reference layers, the unique field and canonical Array/ArrayFixed
type, scalar index/result types, absence of implicit conversions, absence of a
selected index/index-set operation, and the evaluated live local/parameter
owner. That owner is then reused for the inner index.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | The exact local field reborrow has no Pending row. A formal `&mut &mut int[2][2]` with identical index syntax keeps its index, outgoing and N-STORE refusals. Existing nested-store and field-borrow suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all three probe rows and the same three corpus rows. |
| Census | Exact sweep: 1187 programs, 35 to 34 unfinished, 0 timeouts. Exactly `self_mut_field_reborrow` changes: its index, derived outgoing and N-STORE rows disappear and it moves from rc1 to rc0. No row is gained and no diagnostic-code set changes. |
| Backends | The local field-reborrow store probe exits 0 on VM and LLVM with identical empty output. |
| Native ownership | The final LLVM probe is strict-Valgrind clean: exit 0, empty stdout/stderr, no leak or invalid access. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux retains quality 0.53, coupling 0.08, one cycle, broad complex-function count 1084 and the old cc=167 violation. The isolated production certificate is 98 lines. |

## Limits

This is not a general double-reference content model. A function parameter,
captured value, call result, shared layer, converted expression, selected/user
index or local initialized by any other expression stays refused. The field and
both reference layers must retain their exact original types, and the existing
index certificate still checks the final element/store.

No runtime or backend code changed. The certificate follows the source model:
the local reborrow cannot outlive its field owner, and the owner fact already
comes from that checked borrow. No external cell, buffer alias or runtime
lifetime tracking is invented.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-local-reborrow/`: final
and counterfactual compilers, exact census and comparison, the saved census
runner and identity, focused compiler results, the external-cell canary,
VM/LLVM outputs, strict Valgrind logs and Sentrux output.
