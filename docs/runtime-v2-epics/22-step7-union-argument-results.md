# D2 exact union-argument certificate

Verified 2026-10-09. Base: `a4665334184caf5bdae32f29207ff4cc1fe096e9`.
Source candidate: `c4f62badd015c0241dc37b367fe4b4ab3713ffa1`.

## What changed

The type checker already accepts a concrete tag such as `Some(3:int64)`, or
`nothing`, where a generic call's finalized formal is the exact containing
union `Option<int64>`. Return-origin compared the single-member tag descriptor
to the whole union descriptor as if they had to be identical and stopped before
the selected `Array<T>.push` body could transfer the value.

The original-signature check now recognizes that exact relation. The actual
must be `nothing` or a single-member tag; its declaration name and concrete type
arguments must identify exactly one member of the already bound union. Tag
payloads are restricted to scalar, string or enum descriptors. Aggregate,
array, reference and other loan-capable payloads keep the old refusal.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Exact `Some(int64)` and `nothing` arguments finish. An indexed `&mut` receiver keeps its unrelated generic-argument refusal. `Some(fixed-array-view)` remains refused before it can escape through an external container. Existing alias, moved-receiver, generic-signature and tag-conversion suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores all nine target rows. |
| Census | Exact committed-tree sweep: 1187 programs, 23 to 21 unfinished, 0 timeouts. Only `counted_payload_clone_and_borrow.sg` and `for_in_compare_reads_heap_free_union.sg` change; nine rows disappear, no row or diagnostic-code set appears, and both move rc1 to rc0. Census SHA-256 is `5af67c5c2cc4f34c6359f8d395e0c1cf9cc91dde79b1ab5153f01ac04a70d678`; comparison SHA-256 is `85ef2ba337583d9310339f255c64a9394b5c6600c6fa5fa102ab4ace5e0b84a5`. |
| Binary identity | Candidate binary SHA-256 is `9e7971a140566e7a7ac499f0a629f658b43143fc5394029bc2e9e3eccbda4bbb`; census identity records the candidate commit and an empty diff. |
| Backends | Both admitted goldens exit 0 on VM and LLVM with byte-identical output and empty stderr. Output SHA-256 values are `526c6c12dff4325718f6c1fb949de4897bf05e225bee08fc478091961c502bc8` and `f9dae80691ff3e71a7e3146d1ef14aaea300270dc51170757254857b37bfc528`. |
| Native ownership | `for_in_compare_reads_heap_free_union` is strict-Valgrind clean. `counted_payload_clone_and_borrow` loses one 24 B bigint block and is retained as RV2-DEBT-474. The exact preceding compiler accepts a control with the same counted union/borrowed clone but no newly admitted union pushes and loses the identical 24 B. Direct big-int, direct `Pair` and held-without-clone controls are strict-zero. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1086 and the old cc=167 maximum, identical to this packet's base. |

## Limits

This is an original-call type-authority rule, not a general union coercion. It
requires the already selected generic body and its exact bound formal. Only one
exact tag member or `nothing` is admitted, and only for payloads which cannot
hide a reference or storage loan under the supported type shapes. Sibling tags,
unrelated unions, ambiguous members, aggregates, arrays, references and indexed
receiver mismatches retain their existing refusals.

No runtime or backend code changed. The VM/native agreement proves the admitted
call shape; the separately recorded 24 B residue prevents treating the counted
payload golden as native ownership acceptance.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-union-argument/`: the
candidate binary and exact identity, census/comparison, VM/LLVM outputs, strict
Valgrind logs, preceding-compiler counted-union control, direct controls and
Sentrux output.
