# D2 term value-transfer certificates

Verified 2026-10-09. Base: `afca4bef09c7861eff46b8119fbbaff7bd772328`.
Source candidate: `edd7d4bb1fb4274987690740ec250b6473ad8acf`.

## What changed

Two general transfers blocked each standalone term root.

First, a checked non-generic source-body operation was accepted only when its
result was erased. Core's `string.__to(self: &string, _: byte[]) -> byte[]`
builds a fresh byte array, but the array is a loan carrier by type. A source-body
operation may now return a dynamic array when its concrete element can retain
neither a borrow nor a storage loan and the checked summary names no source.
For a cast, the second `__to` formal equal to the result type is recognized as
the unevaluated type marker rather than as a container input. A conversion body
returning a fixed-field window keeps its source and remains refused.

Second, return-origin admitted an inert counted-handle capture only for `async`,
although MIR's retained-capture ownership, blocking-state descriptors and the
native blocking pool use the same counted-handle contract. A canonical counted
handle with exactly one inert payload is now admitted in `blocking` as well.
Crossing/task checks still reject non-inert payloads before this transfer.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Fresh byte-array conversion is clean; a user conversion returning a fixed-field window keeps conversion/outgoing refusals. Blocking `Channel<int64>` capture is clean; wrapped, mutable and invalid-payload channel canaries stay red. Body-conversion, task-block and async-channel suites PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Counterfactual | The exact preceding compiler restores both `term.sg` rows and all three root records. |
| Census | Exact sweep: 1187 programs, 28 to 25 unfinished, 0 timeouts. Nine records change; 31 rows disappear and none appear. The three standalone term roots move from two rows to rc0. Fresh array results also reduce `byte_lines` 13 to 1, each JSON root 73 to 71, the JSON method golden 75 to 73, and the fixed-array operator golden 17 to 12. No diagnostic-code set changes. |
| Backends | The fresh byte-array conversion probe exits 0 on VM and LLVM. VM explicitly rejects `blocking` with FUT7008; no VM blocking acceptance is claimed. The native combined conversion/channel-blocking probe exits 0 with `SURGE_THREADS=2`. |
| Blocking ownership | `TestRuntimeV2BlockingRetainedCaptureCensusBalanced` PASS, including the refused `Channel<int>` control and the running `Channel<int64>` witness. A blocking-only native probe is Valgrind-clean for definite/indirect loss and invalid access; full all-kind accounting retains known runtime reachable/TLS state. |
| Conversion ownership | **FAIL, retained:** the native conversion probe loses one 24 B array header. The preceding compiler already accepts the direct `.__to(marker)` spelling of the same body and loses the identical 24 B, proving the runtime/lowering defect predates this cast certificate. RV2-DEBT-470 owns it. |
| Broad check | The recent pre-commit `make check` remains red on the known return-origin-gated baseline. This packet does not claim it is green. |
| Structural | Sentrux remains at quality 0.53 (5312), coupling 0.08, one cycle, broad complex-function count 1085 and the old cc=167 maximum, identical to this packet's base. |

## Limits

The fresh-result rule requires a checked, non-generic source body, an exact
selected operation, an empty summary, a canonical dynamic array and a concrete
element with no reference or nested storage loan. It does not accept views,
fixed arrays, unknown templates or arbitrary container formals.

The blocking rule applies only to the interner's canonical refcounted-handle
identity with one crossing-inert payload. It does not admit references, wrapped
handles, loan-carrying payloads, non-counted runtime handles or `Channel<int>`
whose arbitrary-precision payload remains unsafe across the blocking boundary.
The VM has no blocking pool.

The real terminal read loop was not executed interactively. Source analysis is
exact for the three roots; runtime evidence uses the same conversion and
counted-channel capture shapes without terminal I/O.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-term/`: final and
counterfactual compilers, exact census/comparison and identity, conversion and
blocking probes, VM/LLVM outputs, blocking census test output, Valgrind logs,
the preceding-compiler direct-conversion control, focused compiler tests and
Sentrux output.
