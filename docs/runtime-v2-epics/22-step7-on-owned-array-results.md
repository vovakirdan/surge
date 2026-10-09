# D2 owned-array `on` capture certificate

Verified 2026-10-09. Base: `67a7f69f3a670e29fbfa2ffc4e7568b157a4fd05`.
Source candidate: `1480936ea8cdf5e3a61fcc6f4d9094dfdd08f224`.

## What changed

The crossing checker already records a legal dynamic-array capture as the exact
`MoveOwned + OwnedMovableElements` verdict. Return-origin nevertheless refused
every non-inert capture by type, including a fresh owned array literal whose
caller binding ends at the crossing.

For an immediate placement `on`, return-origin now accepts that one recorded
verdict only when the capture still names the exact checked identifier, its
symbol/type/span agree, the binding was initialized directly by a typed dynamic
array literal in the same function, and the current binding fact contains no
origin root or callable. The body then owns the moved array. Parameter arrays,
aliases/call results, views and arrays carrying loans do not satisfy this proof.
Far-handle anchored bodies remain on their separate capture/channel contract.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Fresh owned literal capture finishes. An array parameter keeps the capture refusal; anchored array send keeps both capture and channel refusals; `own` reference captures remain refused. Crossing checker mode/verdict tests PASS. |
| Compiler packages | Focused `internal/sema` and `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | Restoring only the previous type-only capture fence restores the exact single `on_positive_dynamic_array_capture` row. |
| Census | Exact clean-candidate sweep: 1187 programs, 37 unfinished, 0 timeouts. Exactly `on_positive_dynamic_array_capture` loses its one row and moves to rc0; no row is gained and no other status, code or reason set changes. |
| Runtime model | Runtime V2 section 5 says the shard boundary is move-only and owned payloads use typed carriers; Epic 11 ON-CAP-V005 says an owned dynamic array crosses when every element may move, and records that the body drops it after the caller binding ends. |
| Backend limit | No backend currently executes placement `on`: the VM request stops at FUT7014; the LLVM request reaches its guard and panics `on placement is not supported by this backend`. No VM/native or Valgrind acceptance is claimed. |
| Structural | Sentrux keeps quality 0.53, coupling 0.08, one cycle and the old max-cc violation; broad complex-function count rises from 1083 to 1084. The changed sema file is 146 lines and passes the size gate. |

## Limits

This certificate is source-analysis and guard-before-HIR evidence. It does not
prove cross-shard transport, because that backend is unavailable. It does not
admit a moved array parameter: a caller may supply a view, so its Param origin
keeps the refusal. It also does not affect `spawn on`, anchored far-handle
bodies, crossing replies or channel payloads.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-on-owned-array/`:
candidate/counterfactual compilers, exact diagnostic records, the backend-
unavailable executable probe, focused tests, Sentrux output and the resumable
exact census.
