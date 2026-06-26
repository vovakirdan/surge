# Epic 1 Task 5: Baseline Evidence Refresh

This file records the current checkout before Epic 2 changes runtime
structure. It is evidence only; no runtime source code was changed.

## Checkout

- Date/time: 2026-06-26T12:44:28+03:00
- Git HEAD: `1755276be886fd5d36f64c1682188cade4625a76`
- Branch: `codex/runtime-net-scheduler-refactor`
- Head summary: `1755276b docs(runtime): refine Runtime V2 shard crossing contract`

`git status --short` at the start of the run:

```text
?? docs/runtime-v2-epics/
```

The `docs/runtime-v2-epics/` directory was already untracked in this checkout.
The baseline subtask wrote this evidence file first; parent integration links
it from the shared epic documents and notes after review.

## Sentrux Baseline

| Scope | Path | Result | Duration | Blocker status |
| --- | --- | --- | ---: | --- |
| Repository scan | `/home/zov/projects/surge/surge` | `quality_signal=6210`; `files=4713`; `import_edges=1887`; `lines=367877` | 2.33s | Does not block by itself. |
| Repository health | `/home/zov/projects/surge/surge` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4696`, modularity `3435`, redundancy `8588`; cross-module edges `1820` | <1s | Does not block by itself. |
| Repository `check_rules` | `/home/zov/projects/surge/surge` | No rules file at `/home/zov/projects/surge/surge/.sentrux/rules.toml`. | <1s | Recorded by Task 3 as an open rule-enforcement blocker; runtime-code completion needs rules or an explicit deferral. |
| Runtime scan | `/home/zov/projects/surge/surge/runtime` | `quality_signal=5147`; `files=32`; `import_edges=30`; `lines=14883` | <1s | Does not block by itself. |
| Runtime health | `/home/zov/projects/surge/surge/runtime` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4735`, modularity `3333`, redundancy `2574`; cross-module edges `0` | <1s | Does not block by itself. |
| Runtime `check_rules` | `/home/zov/projects/surge/surge/runtime` | No rules file at `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`. | <1s | Recorded by Task 3 as an open rule-enforcement blocker; runtime-code completion needs rules or an explicit deferral. |

## Required Commands

| Command | Result | Duration | Blocker status |
| --- | --- | ---: | --- |
| `git diff --check` | Passed with empty output. | <1s | Does not block. |
| `make c-check` | Passed. `clang-format --dry-run --Werror` passed, strict C warning compile passed, and `All C runtime checks passed`. | 2.87s | Does not block. |
| `make cppcheck` | Passed. `cppcheck` scanned 28 native C files and ended with `cppcheck OK`. | 11.90s | Does not block. |
| `go test ./internal/vm -run 'MT\|Async\|Net\|LLVM'` | Failed. Package output ended with `FAIL surge/internal/vm 110.761s`. | 110.76s package time | Accepted backend-test debt for a future test-matrix epic; does not block Epic 2 start. |
| `make check` | Passed. It ran `go test ./... --timeout 90s` with `SURGE_SKIP_TIMEOUT_TESTS=1`, `golangci-lint`, `make c-check`, and `check_file_sizes.sh`. | 13.26s | Does not block. |

Post-review differentiator checks:

| Command | Result | Duration | Meaning |
| --- | --- | ---: | --- |
| `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./internal/vm -run 'MT\|Async\|Net\|LLVM' --timeout 90s` | Passed. Output: `ok surge/internal/vm 1.260s`. | 1.63s wall | Matches the `make check` test environment for timeout-sensitive tests. |
| `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./internal/vm -run 'LLVMParity\|MTCorrectnessHTTPServer\|VMTerm' -v --timeout 90s` | Passed by skipping the matching timeout-sensitive tests. | <1s | Proves the failed cases below are not exercised by default `make check`. |

Focused VM failure summary:

- `TestLLVMParity` failed on exit-code mismatches for `select_channel`,
  `select_timeout`, `fs_dir_smoke`, `fs_metadata_smoke`, `file_rw_smoke`,
  `head_tail_text`, and `walkdir_for_in`.
- `TestLLVMParity` also reported stderr mismatches for `random_pcg32`,
  `hash_xxh64`, `hash_stable64`, and `uuid_v4`: VM reported
  `panic VM1003: expected numeric, got biguint and int`; LLVM reported
  `panic VM3203: division by zero`.
- Several HTTP LLVM parity fixtures failed to build with diagnostics including
  `member "next" of module "stdlib/http" is not public or not a function` and
  `cannot send 'conn' of @nosend type 'TcpConn' to spawn; use @local spawn`.
- `TestLLVMParity/http_server` failed its keepalive scenario because the client
  could not connect after those diagnostics.
- The run also printed `panic: boom`.
- `TestVMTermReadEventQueue` and `TestVMTermSizeOverride` failed MIR
  validation with unknown local types.
- `TestVMTermCallsLog` failed with `panic VM1003: exit requires 1 argument`.
- `TestMTCorrectnessHTTPServer` failed LLVM build and left artifacts under
  `/home/zov/projects/surge/surge/target/debug/.tests/TestMTCorrectnessHTTPServer`.

`make check` passed despite the focused command failure because `Makefile`
sets `SURGE_SKIP_TIMEOUT_TESTS ?= 1`, and the `test` target passes that value
to `go test ./... --timeout 90s`. `internal/vm/test_helpers_test.go`
`skipTimeoutTests` treats any non-empty value other than `0` or `false` as a
skip request. The failed focused command ran without that environment override,
so it exercised timeout-sensitive LLVM parity, MT HTTP, and VM terminal tests
that default `make check` skips. This baseline should keep both facts visible.

## Native Benchmarks

The benchmark scripts first ran with the default `./surge` binary, but the
generated reports showed that binary was from commit `89a730cce51a`, not the
current checkout. Those first reports were discarded as stale baseline evidence.

To measure this checkout, a temporary compiler binary was built outside the
repository:

```text
go build -ldflags "$(./scripts/ldflags.sh --local)" -o /tmp/surge-baseline-1755276b/surge ./cmd/surge/
```

`/tmp/surge-baseline-1755276b/surge version --full` reported commit
`1755276be886`, matching the checkout. The scripts were rerun with
`SURGE=/tmp/surge-baseline-1755276b/surge`.

| Command | Result | Duration | Report |
| --- | --- | ---: | --- |
| `SURGE=/tmp/surge-baseline-1755276b/surge ./scripts/bench_native_net.sh` | Passed. | 9.16s | `/home/zov/projects/surge/surge/build/benchmarks/native-net-request-reply.md` |
| `SURGE=/tmp/surge-baseline-1755276b/surge ./scripts/bench_native_channels.sh` | Passed. | 22.16s | `/home/zov/projects/surge/surge/build/benchmarks/native-channel-request-reply.md` |

### Native Net Request/Reply

Report generated at `2026-06-26T09:49:09Z`.

Environment summary:

- surge commit: `1755276be886`
- fixture: `benchmarks/native/net_request_reply`
- threads: `1 2 4 8`
- modes: `echo direct manager`
- patterns: `seq pipe`
- requests: `2000`
- pipeline depth: `64`

Key result rows:

| threads | mode | pattern | avg us/op | p50 us | p95 us |
| ---: | --- | --- | ---: | ---: | ---: |
| 1 | echo | seq | 79.04 | 62.33 | 147.32 |
| 1 | echo | pipe | 23.43 | 22.38 | 28.61 |
| 1 | direct | seq | 101.79 | 89.22 | 159.93 |
| 1 | direct | pipe | 63.55 | 62.88 | 72.32 |
| 1 | manager | seq | 111.91 | 102.73 | 176.90 |
| 1 | manager | pipe | 72.70 | 73.04 | 76.05 |
| 8 | echo | seq | 90.01 | 62.67 | 259.11 |
| 8 | echo | pipe | 37.29 | 37.15 | 46.48 |
| 8 | direct | seq | 129.79 | 99.45 | 310.19 |
| 8 | direct | pipe | 81.12 | 80.65 | 86.77 |
| 8 | manager | seq | 182.16 | 152.95 | 353.14 |
| 8 | manager | pipe | 111.41 | 111.15 | 118.85 |

Key trace counters:

- `manager` mode records `handoff yields` near one per request:
  `2000` for 1-thread runs and `1999` for 2/4/8-thread runs.
- `task-context blocking sends`, `task-context blocking recvs`,
  `compensation started`, and `compensation high-water` stayed `0` for all net
  rows.
- `poll allocs` stayed `2` for all net rows.
- `net waiter entries` ranged from `21` to `5038`; the 2-thread `echo seq`
  row recorded `net waiter entries=523`.
- 1-thread `manager seq` recorded `sched inject=17761`,
  `net direct waits=1752`, `waiter scan entries=16990`, and
  `completed waiters=1752`.
- 8-thread `manager pipe` recorded `sched inject=4041`,
  `net direct waits=27`, `waiter scan entries=114`, and
  `completed waiters=27`.

### Native Channel Request/Reply

Report generated at `2026-06-26T09:49:28Z`.

Environment summary:

- surge commit: `1755276be886`
- fixture: `benchmarks/native/channel_request_reply`
- modes: `1 2 4 8 default`
- trace probes:
  `channel_ping_pong channel_reused_reply channel_new_reply channel_sync_new_reply`
- channel wake policy: `handoff-inject`

Key result rows:

| mode | probe | iterations | ns/op |
| --- | --- | ---: | ---: |
| 1 | empty_loop | 20000 | 486 |
| 1 | response_bytes | 20000 | 25673 |
| 1 | channel_ping_pong | 20000 | 4199 |
| 1 | channel_reused_reply | 20000 | 3335 |
| 1 | channel_new_reply | 20000 | 4062 |
| 1 | channel_sync_new_reply | 5000 | 9077 |
| 8 | channel_ping_pong | 20000 | 9835 |
| 8 | channel_reused_reply | 20000 | 9380 |
| 8 | channel_new_reply | 20000 | 10596 |
| 8 | channel_sync_new_reply | 5000 | 230791 |
| default | channel_ping_pong | 20000 | 10587 |
| default | channel_reused_reply | 20000 | 9304 |
| default | channel_new_reply | 20000 | 10546 |
| default | channel_sync_new_reply | 5000 | 772405 |

Key trace counters:

- `channel_reused_reply` and `channel_new_reply` recorded `handoff yields=19999`
  for every mode.
- `channel_sync_new_reply` recorded `task-context blocking sends=5000` and
  `task-context blocking recvs=5000` for every mode.
- `channel_sync_new_reply` recorded `channel blocking waits=0` in mode `1`,
  `9128` in mode `2`, `9084` in mode `4`, `9687` in mode `8`, and `9668` in
  `default`.
- `compensation started` and `compensation high-water` stayed `0` for all
  channel rows.

## Known Debt And Epic 2 Gate Status

Current evidence records accepted backend-test debt and the remaining Runtime V2
gate.

Accepted debt:

- The required focused VM command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in the current
  checkout. This is accepted as backend-test debt because the VM/native/LLVM
  test matrix will be rewritten in a later epic after Runtime V2 contracts are
  more stable.

Blocking items:

- Sentrux `check_rules` cannot pass for either the repository root or
  `runtime/` because no `.sentrux/rules.toml` exists at either scan path.

Non-blocking baseline facts:

- `git diff --check`, `make c-check`, `make cppcheck`, and `make check` passed.
- Native net and channel benchmarks ran successfully against a temporary
  current-checkout compiler binary.
- The initial stale `./surge` benchmark attempt was corrected before recording
  benchmark metrics.

## Evidence File Verification

`git diff --no-index --check /dev/null docs/runtime-v2-epics/01-baseline-evidence.md`
produced empty output. The command returned exit code `1`, which is expected
for a no-index comparison against `/dev/null`; empty output is the whitespace
signal for this untracked file.
