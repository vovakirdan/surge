# D2 entropy result certificate

Verified 2026-10-09. Base: `45aa90b83a8b903e70632814c9a3461fa70b503f`.
Source candidate: `a45a1f6340574dfd3aa1c9ceeb9b5177ec825316`.

## What changed

The return-origin analysis now recognizes exactly the retained body-less
`rt_entropy_bytes` declaration from `stdlib/entropy/entropy.sg`. Its result is
the existing fresh-container shape `Erring<byte[], Error>`: the runtime allocates
the byte array or Error itself and retains no input. The certificate reuses the
same result walk as `rt_fs_read_file` and `rt_net_read_bytes`, so the byte
element and Error member are still checked rather than erased.

Identity is fail-closed. The declaration must be builtin, intrinsic,
synchronous, body-less, non-generic, have the exact `uint` parameter, have no
receiver/default/variadic slot, come from the exact stdlib source (relative or
absolute source keys normalize to that suffix), and retain its original
publication. A same-name, same-signature intrinsic in a user module keeps the
opaque-result refusal.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Entropy positive/name-control/dependent-module rows PASS; existing container and handle certificate rows and identity mutations PASS. |
| Compiler packages | Full `internal/sema` PASS; focused `internal/driver` PASS; `go vet ./internal/sema ./internal/driver` PASS. |
| Runtime | `TestLLVMParity/entropy_len` PASS on VM and native; recording/replay entropy runtime tests PASS. |
| Counterfactual | Removing only the `rt_entropy_bytes` table row makes the certificate and all six moved census programs red again. The saved run exits 1. |
| Census | Two independent exact-candidate sweeps are byte-identical: 1187 programs, 44 unfinished, 0 timeouts. Against SC-C, exactly six programs move unfinished to `rc=0`: entropy, random, uuid and their three API goldens. No other status, code or reason set changes. |
| Golden generator | Known generator errors fall from 22 to 19. Exactly the entropy, random and uuid API goldens disappear; no new error appears. Strict golden remains RV2-DEBT-421 FAIL. |
| Structural | Sentrux stays at quality 0.53 with the same baseline acyclicity and old `cc=167` violation. |

This packet does not certify arbitrary stdlib intrinsics or arbitrary
`Erring<T, E>` results. It also does not change the runtime implementation of
entropy, its OOM/NULL contract, or general union ownership.

## Runtime ownership limit

The now-admitted native `entropy_len` witness exits 0 and agrees with the VM,
but Valgrind reports 48 B definitely lost: two 24 B allocations from
`rt_tag_alloc <- entropy_make_success_bytes`, one per entropy result. A control
that binds and explicitly drops one result without a `compare` still loses one
24 B envelope, so this is not RV2-DEBT-078's compare duplication.

The same `entropy_len` source compiled by D1 `c7e34178` reports the identical
2 x 24 B loss. The leak therefore predates this return-origin certificate; the
certificate exposes it to D2 rather than introducing it. RV2-DEBT-454 records
the runtime result-envelope ownership gap. No invalid read/write/free was
reported, and payload ownership is independent from the leaked envelope.

RV2-DEBT-309 also remains open: `rt_entropy_bytes` may return NULL after a
runtime allocation refusal and generated code still dereferences the answer.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-entropy-certificate/`:
exact compiler and census hashes, per-file checkpoints, candidate comparison,
counterfactual logs, golden generator output, D1/candidate Valgrind runs and the
drop control.
