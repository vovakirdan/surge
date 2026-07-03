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
| RV2-DEBT-010 | Copied net handles still carry only the raw native fd view. Task 8 added listener/connection owner metadata but deliberately did not add fake generation safety: fd-registry generations protect poll snapshots and waiter completions, not every stale public handle copy. | Open | Future net handle ABI/lifecycle task after fd registry ownership stabilizes; re-evaluate after Epic 6 Task 11/13 | Public net handle operations validate a registry generation or stable handle id before issuing direct fd operations, and stale copied handles cannot act on a reused OS fd. |
| RV2-DEBT-011 | VM LLVM build/test artifacts are keyed by test name under `target/debug/.tests/`, so running overlapping VM build tests for the same test concurrently can delete or race artifact files and produce false failures such as missing `build.stdout`. | Open | Epic 11 test/backend matrix rewrite or earlier test-harness hardening task | VM build/run helpers use per-run unique artifact directories or locking, and duplicate focused VM test commands can run concurrently without artifact collisions. |
| RV2-DEBT-012 | The generated Surge-level heap accounting benchmark crashes under heavier serial allocation pressure: `serial_alloc_free` with `SURGE_HEAP_BENCH_SERIAL_ITERATIONS=200000` reproduced `status=139`, while the stable Task 8 default `100000` iteration run passes. | Open | Epic 5 Task 8 follow-up or Epic 11 test/backend/perf investigation, before promoting this benchmark beyond manual evidence | The crash is minimized or explained, and either a stable high-pressure heap benchmark replaces the current default or the benchmark is explicitly documented as current-only manual evidence with safe limits. |
| RV2-DEBT-013 | `stdlib/http/server.sg` currently sends raw `TcpConn.__opaque` handles through a channel to worker tasks. Under `SURGE_SHARDS>1`, those workers may not run on the accepted connection's owner shard, so read/write would violate the Epic 6 owner-shard contract unless guarded or redesigned. Task 9 deliberately deferred the runtime rejection guard: copied/raw `TcpConn` operations still need a stable owner/generation guard before denial can be added without breaking current owner-local accept flow. | Open | Epic 6 Task 13 for owner guards/tests, or later stdlib owner-local server design if the runtime guard intentionally rejects this pattern first | Non-owner `TcpConn`/`TcpListener` use is rejected or proven owner-local under `SURGE_SHARDS>1`, and the HTTP server path either runs handlers on owner shards or is documented/tested as single-shard compatibility until a stdlib redesign. |

## Closed Debt

| ID | Debt | Closed By | Evidence |
| --- | --- | --- | --- |
| RV2-DEBT-008 | Sentrux `check_rules` reported missing rules for repository root, `runtime/`, and `runtime/native/`. | Pre-Epic 4 quality hardening | Added `.sentrux/rules.toml`, `runtime/.sentrux/rules.toml`, and `runtime/native/.sentrux/rules.toml`; `sentrux check .`, `sentrux check runtime`, `sentrux check runtime/native`, and MCP `check_rules` for all three paths passed locally. |
| RV2-DEBT-009 | LOC checker ignored C/H runtime files, so CI did not mechanically protect native runtime file growth. | Pre-Epic 4 quality hardening | `check_file_sizes.sh` now checks `go,c,h` by default, prunes generated dirs, and enforces `.loc-legacy-allowlist`; `./check_file_sizes.sh -a` and `make check` passed. |
| RV2-DEBT-014 | `check_file_sizes.sh` counted comment-only lines as LOC, which pressured runtime work to shorten useful design/invariant comments just to satisfy the gate. | Epic 6 tooling hardening | `check_file_sizes.sh` now counts effective source LOC for `.go`, `.c`, and `.h`: blank lines and comment-only `//`/`/* ... */` lines are ignored, while code-bearing lines with trailing comments still count. `./check_file_sizes.sh --self-test` covers Go/C/H comment cases and `./check_file_sizes.sh` passed on the current runtime worktree. |
