# D2 implicit string-temporary certificate

Verified 2026-10-09. Base: `ec522da8d90deded9620e565ffcee2ca45026ed5`.
Source candidate: `dd2be46d501e3f54395d2c3c241a94987f4009d4`.

## What changed

The checker materializes a string rvalue passed to `&string` and intentionally
records no place borrow. Return-origin previously accepted that only when a
core-signature shape proved the call could retain nothing; all other literals,
concatenations and range results stopped at `implicit borrow lacks an admitted
borrow for this expression` even though MIR owns a statement temporary.

Such an rvalue now receives the existing `Temporary(expr)` root. It may flow
through calls inside the same statement. If a binding, task, pointer, external
cell, backing or exit still holds it at statement end, the existing close step
reports `borrowed temporary is kept past the statement that owns it`. A call
that consumes it leaves no root. The recognized rvalues remain bounded to the
existing literal/cast/call/concatenation set plus a newly certified core string
range index.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Twenty-four string-temporary rows PASS. Same-statement reference chains and harmless user/core wrappers finish. Task, raw-pointer and hidden-handle paths keep the exact temporary-kept refusal. Opaque mutable effects retain their independent effect rows. A foreign/reserved checker borrow record remains authoritative and keeps the old evidence refusal. Spawn, select, task-block, crossing and channel canaries PASS with the new precise verdicts. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` suites PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores both custom-probe implicit-borrow rows and the exact prior census records. |
| Census | Exact sweep: 1187 programs, 33 to 28 unfinished, 0 timeouts. Ten records change: 78 old implicit-borrow rows disappear and 12 precise temporary-kept rows appear. Five programs move to rc0: trim/split/join, bigint stress, standalone path, and the two strings goldens. `byte_lines` loses three rows; each JSON root loses 17 old rows and gains three precise rows, while the JSON method golden moves 89 to 75. No diagnostic-code set changes. |
| Backends | The four newly admitted executable roots tested (`15_trim_split_join`, `21_bigint_stress`, `strings_rope_std`, `strings_std`) exit 0 on VM and LLVM with byte-identical output per root. |
| Native ownership | A probe combining literal-to-split, range-result-to-reference and literal scalar index exits 0 on both backends and is strict-Valgrind clean with empty stderr. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1085 and the old cc=167 maximum, identical to this packet's base. |

## Limits

This does not invent a place or extend the temporary's lifetime. Only a shared
`&string` formal with no checker borrow record may take this route. Mutable
references, non-string values, implicit conversions not already represented by
the rvalue transfer, and every expression outside the reviewed forms keep their
old obligations. A present borrow record is never replaced.

The three JSON literals which reach a mutable parser effect now carry
temporary-kept rows rather than the missing-evidence row; the surrounding
mutable-effect refusals remain. They are not silently accepted. The result adds
no runtime lifetime state and follows the source rule that a temporary cannot
survive its statement.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-string-temporary/`: final
and counterfactual compilers, exact census/comparison and identity, the focused
temporary/control suites, four VM/LLVM parity pairs, the strict-Valgrind probe
and Sentrux output.
