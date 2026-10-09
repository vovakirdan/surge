# G5: counted local async capture ownership

Verified 2026-10-09. Source candidate: `fe0d01bd58f04829497cc6600c81e96ec4a034ab`.
Base: `13873c623dc0da6a24c8eaa913ed39d4a9ceba6b`.

## What changed

A local `async { ... }` may receive a return-origin certificate for its own
copy of a canonical counted `Channel<T>` when `T` passes the existing inert
payload check. Borrowed captures, nested runtime handles, task payloads,
blocking bodies and non-canonical declarations remain refused.

The synchronous task constructor borrows a counted caller copy and retains the
frame's copy. The body releases that owner when it starts; cold and cancelled
frames use the existing state cleanup. The same ownership balance repairs the
previous counted scalar capture leak without widening return-origin admission.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused compiler/gate tests | 55 selected tests PASS; full gatecheck 144 PASS. |
| Runtime matrix | 27 sources on both sides; base accepts 9 and candidate 27. All 36 accepted observations build and run with exact native/VM output and exit. |
| Memory | No invalid memory in the accepted matrix. Repaired counted scalar captures move from 20 B definite loss to 0 B definite/indirect. Existing TLS/reachable state and RV2-DEBT-121's 17 B identity-cast loss remain. |
| Counterfactuals | Admission, constructor ownership and body cleanup each go red when reverted. Constructor/body mutations repeat three times with 744 B definite + 20 B indirect loss; all three restored runs have 0 B definite/indirect loss. The external judge rejects non-leak Memcheck errors. |
| Lifecycle | VM and native normal, cold, drop-outer, cancel and owning-payload rows pass. The parked observer proves all new recv/send captures 3/3 at workers 1 and 8; one named control was RUNNING at observation and is not claimed as forced-park evidence. |
| Sanitizers | All 38 approved jobs actually PASS, 19 per side, including preflight planted controls. The 22 sanitizer jobs use the approved unlimited address-space profile; 16 retain the 8 GB cap. |
| Integration | Complete VM/LLVM/pending/MT inventories have no old status change or removal. Aggregate stays 7 PASS / 14 baseline FAIL on both sides with equal full reasons. |
| Census | 1187 records; unfinished 52 to 50, exactly `t22_select_wait_recv` and `t23_select_wait_timer`; no other strict-field change. |
| Golden | The same two expected t22/t23 generator errors vanish. Existing 65 stale paths and 22 remaining generator errors remain; strict S-GOLD is still RV2-DEBT-421 FAIL. |
| Lint/size/review | Candidate lint equals the 169-issue baseline; selected lint repair tests are 53 PASS plus two package PASS; size gate has zero violations; independent non-author review APPROVE. |

The relative packet verdict is PASS. It does not mean the repository is fully
green, close all of RV2-DEBT-103, or close D2. RV2-DEBT-121, 244 and 261 remain
open, and fifty return-origin programs remain unfinished.

## Concurrent counted-scalar finding

An additional direct-capture stress probe found a native SIGSEGV. Exact A/B
shows it is pre-existing: base and candidate each pass 10/10 with one carrier,
fail 9/10 with SIGSEGV at eight carriers, and pass on the VM. GDB reaches
`trim_len` through `rt_bigint_cmp` with a corrupted freed block. ThreadSanitizer
names two carrier threads racing in `rt_bigint_release` on the same 20-byte
block.

This is the gap between Epic 22's non-atomic-count ruling and the transitional
`1 shard x N carriers` topology: a deep copy only at a shard crossing does not
separate two tasks executing on different carrier threads of the same shard.
It is recorded as RV2-DEBT-452. This packet neither changes the numeric count
nor claims to close that defect. G5's newly certified Channel handle has an
atomic handle count and an inert payload boundary; the direct counted-scalar
sharing program already builds and fails at the base.

## Evidence and publication boundary

The G5 packet is retained under
`~/.cache/surge-artifacts/step7-return-source/20261008-g5/`. The crash A/B,
compiler/source/binary hashes, GDB evidence and TSan report are under
`~/.cache/surge-artifacts/step7-return-source/20261009-g5-crash/`.

Publication is limited to `validation/step7-d2-on-d1`. The source tree must
equal the frozen `fe0d01bd` candidate before the documentation commit is made.
No Co-Authored-By line is used.
