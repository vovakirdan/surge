# Runtime V2 Debt Ledger

This is the durable debt ledger for Runtime V2 work. Add new debt here when it
is discovered. Close debt only with evidence: commit, task, command, or linked
document.

## Rules

- Do not hide debt in `NOTES.md` only. `NOTES.md` is a handoff log; durable debt
  belongs here.
- Every debt item must have an owner: an epic, task, or explicit future
  decision point.
- Every runtime task must state whether it touches any open debt item.
- Closing debt requires evidence and a dated note.
- Raising a legacy LOC ceiling in `.loc-legacy-allowlist` is a debt decision and
  must update this file or the owning task evidence.

## Open Debt

| ID | Debt | Status | Owner | Close Condition |
| --- | --- | --- | --- | --- |
| RV2-DEBT-001 | Broad focused VM/backend command `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails when timeout-sensitive paths are not skipped. | Planned | Epic 11 test/backend matrix rewrite | Stable Runtime V2 contracts exist, the VM/native/LLVM matrix is rewritten, and the broad diagnostic command is either green or replaced by exact CI gates. |
| RV2-DEBT-002 | Timeout-sensitive tests `TestMTBlockingChannelHelpersDoNotParkWorkers` and `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` are excluded from current green gates. | Planned | Epic 11, or earlier owner task if sync-helper/compensation semantics change | Tests are stabilized, rewritten, or replaced by focused probes that cover the same contract. |
| RV2-DEBT-003 | `runtime/native/rt_async_state.c` remains over the Runtime V2 line target. | Open | Future scheduler/timer/shutdown refactor tasks | File is split by dependency boundary and removed from `.loc-legacy-allowlist`. |
| RV2-DEBT-004 | `runtime/native/rt_net.c` remains over the Runtime V2 line target. Epic 4 Task 14 made partial progress by extracting `TRACE_NET` into `rt_net_trace.c/h` and lowering the legacy ceiling from `1002` to `904`, but the file still exceeds 500 lines. | Open | Future net wake-fd, poll-construction, or net-handle lifecycle split after Epic 4 closeout | Remaining net code is split by dependency boundary and `runtime/native/rt_net.c` is removed from `.loc-legacy-allowlist`. |
| RV2-DEBT-005 | Non-Epic-4 native runtime files remain over the hard LOC gate: `rt_term.c`, `rt_fs.c`, `rt_async_task.c`, `rt_string.c`, `rt_bignum_int.c`, and `rt_bignum_uint_div.c`. | Open | Later runtime cleanup or owning feature epics | Each file is split by responsibility and removed from `.loc-legacy-allowlist`. |
| RV2-DEBT-006 | Channel benchmark script still relies on outer timeout wrappers instead of per-probe timeout ownership. | Open | Benchmark tooling task before the next performance-sensitive channel epic | `scripts/bench_native_channels.sh` owns per-probe timeout handling and reports probe/mode on timeout. |
| RV2-DEBT-007 | Sentrux complexity/function-length thresholds are calibrated to current legacy ceilings, not ideal Runtime V2 targets. | Open | Later quality-hardening pass after large-file refactors | `max_cc` and `max_fn_lines` are lowered without causing baseline violations. |
| RV2-DEBT-010 | Copied net handles still carry only the raw native fd view. Task 9 protects stale poll snapshots and waiter completions from numeric fd reuse, but a stale copied `TcpConn`/`TcpListener` handle is not yet generation-aware. | Open | Future net handle ABI/lifecycle task after fd registry ownership stabilizes | Public net handle operations validate a registry generation or stable handle id before issuing direct fd operations, and stale copied handles cannot act on a reused OS fd. |

## Closed Debt

| ID | Debt | Closed By | Evidence |
| --- | --- | --- | --- |
| RV2-DEBT-008 | Sentrux `check_rules` reported missing rules for repository root, `runtime/`, and `runtime/native/`. | Pre-Epic 4 quality hardening | Added `.sentrux/rules.toml`, `runtime/.sentrux/rules.toml`, and `runtime/native/.sentrux/rules.toml`; `sentrux check .`, `sentrux check runtime`, `sentrux check runtime/native`, and MCP `check_rules` for all three paths passed locally. |
| RV2-DEBT-009 | LOC checker ignored C/H runtime files, so CI did not mechanically protect native runtime file growth. | Pre-Epic 4 quality hardening | `check_file_sizes.sh` now checks `go,c,h` by default, prunes generated dirs, and enforces `.loc-legacy-allowlist`; `./check_file_sizes.sh -a` and `make check` passed. |
