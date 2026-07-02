# Epic 4 Evidence

This file is the task evidence ledger for Epic 4, persistent fd registry and
net lifecycle ownership. Keep entries short, exact, and command-backed.

## Starting Evidence

- Epic 3 is complete for owner-local waiters and dependency-aware runtime
  refactoring under `N=1`.
- Persistent fd registry, net lifecycle ownership, accept distribution, `N>1`,
  crossing syntax, and backend I/O changes were not implemented in Epic 3.
- Current net polling still builds temporary fd rows from net waiter state in
  `runtime/native/rt_net.c`.
- Current known line-count debt from Epic 3 closeout:
  - `runtime/native/rt_async_state.c`: 1731 lines;
  - `runtime/native/rt_net.c`: 1024 lines;
  - `runtime/native/rt_async_trace.c`: 497 lines;
  - `runtime/native/rt_async_internal.h`: 499 lines.
- The broad focused VM command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted
  backend-test debt and is not an Epic 4 green gate.
- Sentrux rule files were added during pre-Epic 4 quality hardening for root,
  `runtime/`, and `runtime/native`.

## Task Evidence Ledger

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Kickoff baseline, Sentrux state, line counts, and gate plan recorded below. |
| 2 | Complete | FD registry dependency map recorded below. |
| 3 | Complete | FD lifecycle behavior contract tests recorded below. |
| 4 | Complete | Registry static shape tests recorded below. |
| 5 | Complete | Registry container skeleton recorded below. |
| 6 | Complete | Net wait registration through registry recorded below. |
| 7 | Complete | Poll-from-registry migration recorded below. |
| 8 | Complete | Close/cancel/re-register behavior tests recorded below. |
| 9 | Pending | Close/cancel/re-register migration. |
| 10 | Pending | Wake-fd and shutdown behavior tests. |
| 11 | Pending | Wake-fd and shutdown migration. |
| 12 | Pending | Trace counters and benchmark contract. |
| 13 | Pending | CI gate wiring. |
| 14 | Pending | Large-file refactor tranche. |
| 15 | Pending | Closeout gates and handoff. |

## Task 1 Evidence: Kickoff Baseline And Sentrux

Date: 2026-07-02. Docs-only task; no runtime, compiler, test, Makefile, CI, or
Sentrux rule changes.

Checkout state:

- Branch: `codex/runtime-net-scheduler-refactor`.
- Baseline commit: `05ceb7c20b19e72125e320f07445959cb2b349bf`
  (`chore(runtime): enforce Runtime V2 quality gates`).
- `git status --short` was empty before the task.
- `git diff --check` passed with empty output.

Runtime/native line counts at kickoff:

- `runtime/native/rt_net.c`: 1024 (over limit; allowlisted at 1024);
- `runtime/native/rt_async_state.c`: 1731 (over limit; allowlisted at 1731);
- `runtime/native/rt_async_trace.c`: 497;
- `runtime/native/rt_async_internal.h`: 499;
- `runtime/native/rt_async_waiter.c`: 309;
- `runtime/native/rt_runtime.c`: 161.

Sentrux state (CLI; the Sentrux MCP server is not connected in this session,
so `session_start`/`session_end` are replaced by `sentrux gate --save` CLI
baselines; this is recorded as tool-availability state, not rule
non-compliance):

- `sentrux check .`: passed, 10 rules, quality `6198`;
- `sentrux check runtime`: passed, 7 rules, quality `5195`;
- `sentrux check runtime/native`: passed, 7 rules, quality `5159`;
- `sentrux gate --save` recorded baselines `6198` / `5195` / `5159` for the
  three mandatory scan roots.

Startup gates:

- `make c-check`: passed;
- `make cppcheck`: passed (31/31 files);
- `make runtime-v2-check`: first run failed once inside the MT seed set
  (`FAIL surge/internal/vm 38.558s`); a full isolated rerun passed with
  `exit=0` (seed `7.801s`, waiter static `0.033s`, pending waiter set
  `19.130s`). This matches the flake class already recorded in Epic 3 Task 06
  (`TestMTBlockingChannelHelpersAllowTimersToAdvance` internal program
  timeout under load). Recorded as pre-existing flake debt, not a new Epic 4
  regression.
- `make check`: passed, including `check_file_sizes.sh` with the
  `.loc-legacy-allowlist` ceilings.

Accepted debt carried into Epic 4 (unchanged):

- broad focused VM command `go test ./internal/vm -run 'MT|Async|Net|LLVM'`
  stays accepted backend-test debt (RV2-DEBT-001) and is not an Epic 4 gate;
- `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` remain
  excluded from green gates (RV2-DEBT-002);
- `rt_async_state.c` and `rt_net.c` remain over the 500-line target
  (RV2-DEBT-003, RV2-DEBT-004; Task 14 owns the Epic 4 tranche).

Gate plan for Tasks 2-7:

- Task 2 (docs map): `git diff --check` plus targeted `rg` symbol evidence;
  no runtime tests required.
- Task 3 (contract tests): default tag-off proof for the new test file,
  tagged pending proof where tests describe future registry behavior, and
  the focused net probe
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s`.
- Task 4 (static tests): exact
  `go test ./internal/vm -run TestRuntimeV2FDRegistryStatic -v --timeout 90s`
  in its approved tag mode plus `git diff --check`.
- Tasks 5-7 (runtime code): `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, Task 3/4 proofs in their approved
  modes, focused net wake probe `TestMTNetWaiterWakeupLatency`, native net
  benchmark with a current-checkout `SURGE` binary and outer `timeout` for
  Task 7, `git diff --check`, and Sentrux root plus scoped CLI checks with
  the saved gate baselines.

## Task 2 Evidence: FD Registry Dependency Map

Date: 2026-07-02. Docs-only; subagent-executed after an approved plan-only
pass (Global Rule 9). Only `04-fd-registry-dependency-map.md` was created
(390 lines); evidence and notes were recorded by the main session.

- References pinned to baseline `05ceb7c2`; verified `runtime/` identical at
  working tree `d7098fab` via empty `git diff --stat 05ceb7c2 HEAD --
  runtime/`.
- `git diff --check`: clean. New-file whitespace check
  (`git diff --no-index --check /dev/null <map>`): zero complaints.
- Symbol inventories recorded with `rg` evidence:
  - `rt_net_poll_scratch`: 9 hits (type `rt_async_internal.h:139-144`, shard
    field `:156`, decls `:404-405`, accessors `rt_runtime.c:72-80`, users
    `rt_net.c:240/262/921`); no destroy/free path exists;
  - `rt_executor_visit_net_waiters`: 3 hits (impl
    `rt_async_waiter.c:118-134`, decl, sole caller `rt_net.c:928`);
  - net waiter key wakeups: constructors, `waker_is_net`, `net_len`
    bookkeeping, completion keys at `rt_net.c:983-987` and `:1012-1018`.
- Load-bearing facts for later tasks:
  - close paths (`rt_net.c:656/670`) never wake or remove parked net
    waiters and never kick the poller; numeric fd reuse can wake
    old-lifetime waiters (key is the raw fd);
  - `ex->shutdown` has readers at 8 sites but no writer sets it to 1
    anywhere in `runtime/native`; no executor shutdown/drain contract exists
    today (Tasks 10-11 create one for the registry surface);
  - the wake pipe is process-global (`rt_net.c:72-73`), lazily initialized
    inside the poll, written only from `park_current` for net keys;
  - `rt_executor_wake_net_waiters_for_key` is split for migration:
    completion routing becomes registry-owned (T7/T9), task wake stays
    waiter-owned;
  - first safe Task 5 boundary: shard field beside `net_poll_scratch`,
    owner-first accessors, explicit `rt_runtime_status`, zero behavior
    readers; declarations go into new `runtime/native/rt_fd_registry.h`
    included from `rt_async_internal.h` because that header is at 499/500
    lines.

## Task 3 Evidence: FD Lifecycle Contract Tests

Date: 2026-07-02. Test-writing only; subagent-executed after an approved
plan-only pass. Only `internal/vm/runtime_v2_fd_registry_contract_test.go`
was created (499 lines, `//go:build runtime_v2_pending`, package `vm_test`).
No runtime C, Makefile, or CI changes.

Four contract tests, all green against CURRENT behavior (no intentionally
red proofs; they are the behavior Tasks 5-11 must keep green):

- `TestRuntimeV2FDRegistryRepeatedReadinessSingleFD`: 12 ping/pong rounds on
  one connection with a 250ms idle gap forcing a re-park.
- `TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow`: reader parked on
  read interest plus a 32MB bulk writer parked on write interest on the same
  fd under `SURGE_THREADS=1`.
- `TestRuntimeV2FDRegistryDuplicateReadWaitersBothComplete`: two waiters on
  one fd read key; ack protocol is order-independent and stays correct under
  both current wake-all and future wake-one semantics.
- `TestRuntimeV2FDRegistryClosedFDFailsFast`: closed listener/conn handles
  fail fast with a synchronous `NetError`, never parking.

Assertion durability rule applied: only migration-durable counters are
asserted (`io_poll_waiters_max==1` as distinct-fd-row max, `io_poll_calls`,
`io_poll_net_ready`, `io_direct_waits`, `io_waiter_completed`). No
`io_poll_dedup_checks`, `io_waiter_scan_entries`, `io_waiter_net_entries`,
`io_poll_rebuilds`, or `io_poll_allocs` assertions, because those encode the
legacy waiter-scan rebuild path that Task 7 removes.

Recorded deviation: the closed-fd test asserts `NetError` code `>=2`
(neither success nor would-block), not the planned code `5` (NotConnected).
Root cause proven by strace (`close(4)=0` then `read(4)=-1 EBADF`): a Surge
handle copy `{ __opaque: handle }` clones the `NetConn` view, so
`rt_net_close_conn`'s `closed=true; fd=-1` mutation is not visible through
copies and post-close ops map `EBADF` to `NET_ERR_IO` (8). This also
documents a concrete fd-reuse hazard for Tasks 8-9: a live handle copy
retains the raw fd number after close.

Checks (all recorded verbatim by the executor):

- `gofmt -l`: clean; `go vet -tags runtime_v2_pending ./internal/vm`: clean.
- Tag-off proof: `go test ./internal/vm -run '^TestRuntimeV2FDRegistry'
  -count=1 --timeout 60s` -> `ok ... [no tests to run]`.
- Tagged proof (twice, back to back): `SURGE_BACKEND=llvm
  SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm
  -run '^TestRuntimeV2FDRegistry(RepeatedReadinessSingleFD|
  ReadWriteInterestSharesFDRow|DuplicateReadWaitersBothComplete|
  ClosedFDFailsFast)$' -count=1 -parallel=1 -p=1 -v --timeout 180s` ->
  4/4 PASS both runs (15.6s / 15.5s).
- Focused net probe: `TestMTNetWaiterWakeupLatency` PASS (2.43s).
- Exact `runtime-v2-waiter-check` tagged command rerun directly: all 12
  existing tests PASS alongside the new same-tag file.
- `git diff --check`: clean.

## Task 4 Evidence: Registry Static Shape Tests

Date: 2026-07-02. Test-writing only; subagent-executed after an approved
plan-only pass. Only `internal/vm/runtime_v2_fd_registry_static_test.go` was
created (175 lines, `//go:build runtime_v2_pending`). No runtime C,
Makefile, or CI changes.

- `TestRuntimeV2FDRegistryStaticShape`: expected RED until Task 5. Pins the
  approved Task 5 C contract via `clang -std=c11 -Wall -Wextra -Werror
  -fsyntax-only` function-pointer signature pins and `_Static_assert`s:
  `rt_fd_close_state` (`OPEN==0`, `CLOSED` distinct), `rt_fd_entry {int fd;
  uint64_t generation; uint8_t close_state/want_accept/want_read/
  want_write}`, `rt_fd_registry {entries; len; cap}`, by-value
  `rt_shard.fd_registry`, `rt_shard_fd_registry(_const)` and
  `rt_executor_fd_registry(_const)` adapters, and
  `rt_fd_registry_init/free/ensure_cap/len/find_const` with
  `rt_runtime_status`. Verbatim expected failure recorded: leads with
  `<stdin>:6:1: error: unknown type name 'rt_fd_registry'`, 20 errors.
  The snippet includes only `rt_async_internal.h`; Task 5 makes the
  declarations reachable through a nested `rt_fd_registry.h` include.
- `TestRuntimeV2FDRegistryStaticBoundary`: GREEN today. Pins the current
  approved placeholder: `N=1` `#error` guard, shard-owned
  `net_poll_scratch` field and accessors, `poll_net_waiters` /
  `rt_net_wake_poll` signatures, net waker keys plus `waker_is_net`, and
  distinct explicit `rt_runtime_status` codes.
- Mutation APIs (add/remove interest, close, poll-build) are deliberately
  not pinned; Tasks 6/7/9 extend the guard when they land.
- Checks: `gofmt -l` clean; `go vet -tags runtime_v2_pending` clean;
  default-tag run matches zero tests (intended tag-off proof); Boundary
  PASS (0.10s); Shape FAIL as expected; existing pending static tests
  (`OwnerLocalWaiterSkeletonStaticShape`, `SkeletonStaticShape`) PASS;
  default-tag `WaiterHelperStaticBoundary` PASS; `git diff --check` clean.
- Skipped per Rule 6 with reason: `make c-check`/`make cppcheck`/benchmarks
  (no native C changes in Tasks 2-4); Sentrux scans stay owned by the main
  session per task boundaries.

## Task 5 Evidence: Registry Container Skeleton

Date: 2026-07-02. Runtime C skeleton; subagent-executed after an approved
plan-only pass (Global Rule 9). Working tree intentionally left uncommitted:
the main session owns the commit and the Sentrux gates.

Files and line-count outcomes (Global Rule 4):

- NEW `runtime/native/rt_fd_registry.h`: 54 lines. Registry types
  (`rt_fd_close_state`, `rt_fd_entry`, `rt_fd_registry`), the four accessor
  declarations, the five lifecycle/query declarations, and one ownership
  comment block (shard-owned by value; ex->lock-guarded once Tasks 6-9 route
  behavior; `generation`/`close_state` reserved for Task 9 behavior; free has
  no caller until Tasks 10-11 create a shutdown path).
- NEW `runtime/native/rt_fd_registry.c`: 72 lines. `init` zeroes; `free`
  releases entries via `rt_free` and re-zeroes; `ensure_cap` mirrors
  `rt_waiter_store_ensure_cap` (start cap 16, doubling, `SIZE_MAX / 2U` and
  `SIZE_MAX / sizeof(rt_fd_entry)` overflow guards, `rt_realloc`); `len` and
  `find_const` are read-only queries. Explicit `rt_runtime_status`; no
  `panic_msg`; no plain `bool`.
- `runtime/native/rt_async_internal.h`: 499 -> 499, not grown. Budget: +1
  `#include "rt_fd_registry.h"` directly after the `rt_shard`/`rt_executor`
  forward typedefs (the include point needs `rt_runtime_status` and those
  typedefs, and must precede `struct rt_shard`); +1 by-value
  `rt_fd_registry fd_registry;` field beside `net_poll_scratch`; -2 blank
  separator lines (before `rt_channel_blocking_compat` and before
  `struct rt_shard`, matching the compact enum-run style already in the
  file).
- `runtime/native/rt_runtime.c`: 161 -> 184. Four accessors after the
  waiter-store block, copying its exact pattern (null-safe shard accessors,
  shard0-routing executor adapters, `shard_count` guard in the const
  adapter). `rt_runtime_init_n1` now returns
  `rt_fd_registry_init(rt_shard_fd_registry(&runtime->shards[0]))`:
  redundant with the surrounding `memset` today, kept explicit so the
  init/free lifecycle pairing exists and failure status propagates through
  the existing `exec_init_once` panic boundary without adding `panic_msg`
  to the new API or touching over-limit `rt_async_state.c`.
- Untouched: `rt_net.c` (1024), `rt_async_state.c` (1731),
  `rt_async_waiter.c`, Makefile, CI. Zero runtime readers/writers of the
  registry exist; the poll rebuild path is unchanged. Build pickup is
  automatic: `Makefile` `C_SOURCES` uses `find`, and
  `runtime/native_embed.go` embeds `native/*.c native/*.h`.

Checks (all run by the executor, in order):

- `make c-check`: passed (formatting + strict warnings incl. new files).
- `make cppcheck`: passed, 32/32 files, no suppressions added.
- `make runtime-v2-check`: passed (MT seed set plus waiter gate, 12/12
  tagged waiter proofs).
- `make check`: passed; `check_file_sizes.sh` green for all four touched
  C files.
- Shape gate flip: `go test -tags runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistryStaticShape$' -count=1 -v --timeout 90s` ->
  PASS (1.07s; expected-red in Task 4, now green with zero test edits).
- Boundary gate: same command with `StaticBoundary` -> PASS (0.04s).
- Task 3 tagged contract 4-pack, verbatim Task 3 command -> 4/4 PASS
  (15.9s).
- Focused net probe: `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test
  ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s` ->
  PASS (2.37s).
- `clang-format --dry-run --Werror` on all four touched C files: clean.
- `git diff --check`: clean.
- Skipped per approved plan: Sentrux scans (main session owns the epic
  Sentrux CLI check/gate) and the commit (main session owns commits).

Main-session Sentrux results after the Task 5 implementation:

- `sentrux check .`: passed, quality `6198` (stable);
- `sentrux check runtime`: passed, quality `5200` (baseline `5195`, +5);
- `sentrux check runtime/native`: passed, quality `5164` (baseline `5159`,
  +5);
- `sentrux gate runtime/native`: `5159 -> 5164`, coupling `0.00 -> 0.00`,
  cycles `0 -> 0`, god files `0 -> 0`, `No degradation detected`.

## Task 6 Evidence: Net Wait Registration Through Registry

Date: 2026-07-02. Runtime C; subagent-executed after an approved plan-only
pass (Global Rule 9). Working tree intentionally left uncommitted: the main
session owns the commit and the Sentrux gates.

Files and line-count outcomes (Global Rule 4):

- `runtime/native/rt_fd_registry.h`: 54 -> 63. Two new declarations
  (`rt_fd_registry_attach_net_interest` returning `rt_runtime_status`,
  `rt_fd_registry_detach_net_interest` returning void) plus ownership-comment
  updates for the Task 6 write path, the row-lifetime invariant, and the
  generation-reset consequence.
- `runtime/native/rt_fd_registry.c`: 72 -> 154. Static `fd_registry_find_mut`
  and the `key.kind -> want_*` slot switch; attach (find-or-create under
  `ensure_cap`, idempotent flag set, explicit status, allocation can fail only
  on row creation); detach (missing row is a legal no-op; clearing the last
  flag swap-removes the row).
- `runtime/native/rt_async_waiter.c`: 309 -> 381. Bridge statics
  `fd_registry_bridge_net_attach` / `fd_registry_bridge_net_detach_if_last`
  plus four hook sites: `add_waiter` (attach after successful append),
  `remove_waiter` and `pop_waiter` (same-pass `kept_same_key` counting,
  detach only when the last same-key waiter left), and
  `rt_executor_wake_net_waiters_for_key` (all same-key waiters removed by
  construction, detach with remaining 0).
- `runtime/native/rt_async_internal.h`: 499 -> 499, not grown. One line
  edited in place: the executor invariant block now lists `fd registry rows`
  under `ex->lock` ownership.
- Untouched: `rt_net.c` (1024, over-limit file stays flat; zero changes,
  `poll_net_waiters` byte-identical) and `rt_async_state.c` (1731; the
  `park_current` net wake-pipe kick and `io_cv` signal are unchanged).
- `internal/vm/runtime_v2_fd_registry_static_test.go`: 175 -> 185. Shape
  guard extended with function-pointer pins for the two mutators; the
  "deliberately not pinned" comment now scopes to Tasks 7/9.

Design record (Global Rule 2 answers):

- The registry has writers but zero readers in Task 6: poll input remains
  100% waiter-derived, so net poll behavior is preserved by construction.
- Interest lifecycle is exact, not monotonic: interest flags stay `uint8_t`
  0/1 flags (pinned shape, not counts); the "last waiter for key gone"
  decision is made by the waiter store's existing full-store scans in the
  same pass. Duplicate same-key waiters keep interest alive via
  `kept_same_key`.
- Entry removal policy: a row exists iff at least one net-key waiter for that
  fd is parked (modulo attach-miss below). Clearing the last interest flag
  swap-removes the row. Recorded consequence for Task 9: remove-plus-recreate
  resets `generation` to 0; Task 9 owns re-deciding row lifetime when it adds
  generation/close semantics. No generation bumps and no close marking were
  implemented in Task 6.
- Named bridges: `fd-registry-waiter-bridge` (interest mirrors waiter-store
  membership; re-validated or replaced when Task 7 flips poll input and
  Task 9 moves close/cancel ownership) and `fd-registry-attach-miss`
  (allocation failure during attach: status checked inside
  `fd_registry_bridge_net_attach`; the waiter parks on the store without a
  registry row and behavior is unchanged because nothing reads the registry;
  resolver: Task 7).
- No new wake is needed for interest changes: attach happens under `ex->lock`
  inside `add_waiter`, every net park still commits through `park_current`,
  which already writes the wake pipe for net keys and signals `io_cv`
  (`rt_async_state.c:1043-1046`, untouched). Detach never needs a wake in
  Task 6 because registry state is not poll input.
- `wake_key_all_with_policy` net branch intentionally not hooked: grep re-run
  during implementation confirms zero net-key producers. Verbatim commands:
  `rg -n 'wake_key_all' runtime/native` -> producers are
  `scope_key` (`rt_async_state.c:1248`), `join_key`
  (`rt_async_state.c:1371`), and `blocking_key`
  (`rt_async_blocking.c:110`); `rg -n 'add_wait_key\(' runtime/native` ->
  all producers in `rt_async_task.c` build `join_key` /
  `channel_recv_key` / `channel_send_key` only (inspected at
  `:321/:325/:402/:406/:585-656`). The dead net branch remains Task 7
  cleanup debt per the dependency map.
- Debug consistency check: inside `fd_registry_bridge_net_detach_if_last`,
  gated by `rt_async_debug_enabled()` (`SURGE_ASYNC_DEBUG`): independent
  same-key recount cross-checked against the caller's count, plus a
  stale-interest check (flag set with zero waiters). Zero-cost branch when
  debug is off.

Checks (all run by the executor, in order):

- Producer greps above (recorded before edits).
- `clang-format --dry-run --Werror` on all four touched C files: clean;
  `gofmt -l internal/vm`: clean;
  `go vet -tags runtime_v2_pending ./internal/vm`: clean.
- `make c-check`: passed (formatting + strict warnings).
- `make cppcheck`: passed, 32/32 files, no suppressions added.
- `make runtime-v2-check`: passed (MT seed set plus the tagged waiter gate,
  12/12 waiter proofs alongside the fd static gates).
- `make check`: `exit=0`, including `check_file_sizes.sh` (four checked
  files all OK; `rt_net.c` untouched at its 1024 allowlist ceiling).
- Extended static gates: `go test -tags runtime_v2_pending ./internal/vm
  -run '^TestRuntimeV2FDRegistryStatic(Shape|Boundary)$' -count=1 -v
  --timeout 90s` -> Shape PASS (0.11s, now pinning the Task 6 mutators),
  Boundary PASS (0.03s, zero edits).
- Tag-off proof: `go test ./internal/vm -run '^TestRuntimeV2FDRegistry'
  -count=1 --timeout 60s` -> `ok ... [no tests to run]`.
- Task 3 tagged contract 4-pack, verbatim Task 3 command -> 4/4 PASS
  (15.7s; duplicate-waiter test green proves detach does not fire while a
  same-key waiter remains).
- Focused net probe: `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test
  ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s` ->
  PASS (2.46s).
- Single-thread net + sync channel probe: `SURGE_SKIP_TIMEOUT_TESTS=0
  go test ./internal/vm -run
  'TestNativeNetSingleThreadBlockingChannelInAsyncServer' -v --timeout 90s`
  -> PASS (4.47s).
- Debug-path proof: the Task 3 ping fixture was extracted and compiled with
  a current-checkout scratch compiler (`go build ./cmd/surge`); a manual run
  with `SURGE_ASYNC_DEBUG=1 SURGE_TRACE_EXEC=1` over 3 ping/pong rounds with
  a 300ms idle re-park gap exited 0 with zero `fd-registry-bridge mismatch`
  and zero `fd-registry-attach-miss` stderr lines, while the bridge was
  exercised (`io_direct_waits=2`, `io_waiter_completed=2`,
  `io_poll_waiters_max=1`). The gate mechanism was proven live by a separate
  channel fixture emitting `async chan new` under the same env var. The
  `RepeatedReadinessSingleFD` and `DuplicateReadWaitersBothComplete`
  contract tests also PASS with `SURGE_ASYNC_DEBUG=1`.
- `git diff --check`: clean.
- Skipped per approved plan: Sentrux scans and the commit (main session owns
  both); native net benchmark (Task 7 owns the performance-sensitive
  evidence).

Main-session Sentrux results after the Task 6 implementation:

- `sentrux check .`: passed, quality `6196` (Task 1 baseline `6198`, -2;
  root rules still pass, scoped signals govern completion per
  `SENTRUX_POLICY.md`);
- `sentrux check runtime`: passed, quality `5215` (baseline `5195`, +20);
- `sentrux check runtime/native`: passed, quality `5160` (baseline `5159`,
  +1);
- `sentrux gate runtime/native`: `5159 -> 5160`, coupling `0.00 -> 0.03`,
  cycles `0 -> 0`, god files `0 -> 0`, `No degradation detected`.

## Task 7 Evidence: Poll From Registry

Date: 2026-07-02. Runtime C; subagent-executed after an approved plan-only
pass (Global Rule 9). Working tree intentionally left uncommitted: the main
session owns the commit and the Sentrux gates. Start commit `617f8cfa5881`
(`feat(runtime): route net wait interest through fd registry`), clean tree.

Files and line-count outcomes (Global Rule 4; both over-limit files shrink):

- `runtime/native/rt_net.c`: 1024 -> 1002. Deleted `NetPollFd`,
  `NetPollBuildContext`, and `collect_net_poll_fd` (the waiter-visit poll
  build with O(n^2) fd dedup); `poll_net_waiters` now derives capacity from
  `rt_fd_registry_len` and fills the scratch through
  `rt_fd_registry_snapshot_poll_interest`; `ensure_net_poll_fds` re-typed to
  `rt_fd_poll_interest`; `net_wait_current_task` gained the attach-miss
  resolution (below). Allowlist ceiling 1024 untouched; reduction recorded.
- `runtime/native/rt_fd_registry.h`: 63 -> 80. `rt_fd_poll_interest` snapshot
  row type, `rt_fd_registry_net_interest_present` and
  `rt_fd_registry_snapshot_poll_interest` declarations, ownership comments
  updated to the Task 7 state (registry is the only poll input; attach-miss
  resolved).
- `runtime/native/rt_fd_registry.c`: 154 -> 213. `fd_entry_interest_value`
  (read-only twin of the mutable slot map), `net_interest_present`, and
  `snapshot_poll_interest` (one linear pass, rows unique per fd, want_accept
  folds into readable-class want_read; zero-interest skip is defensive only).
- `runtime/native/rt_async_waiter.c`: 381 -> 348. Deleted dead
  `rt_executor_waiter_len`, `rt_executor_net_waiter_len`, and
  `rt_executor_visit_net_waiters`; the Task 6 debug recount now calls
  `rt_fd_registry_net_interest_present` instead of open-coding the kind->flag
  map (Rule 5 dedup); bridge comment updated to the resolved state.
- `runtime/native/rt_async_internal.h`: 499 -> 493. Deleted the
  `rt_waiter_key_visitor` typedef and the three dead declarations.
- `runtime/native/rt_async_state.c`: 1731 -> 1727. Deleted the dead net-key
  `net_len` adjustment in `wake_key_all_with_policy` (dependency-map row
  "registry-owned dead-path cleanup, migrates in T7").
- `internal/vm/runtime_v2_waiter_static_test.go`: 90 -> 86 (pins removed,
  below). `internal/vm/runtime_v2_fd_registry_static_test.go`: 185 -> 206
  (Task 7 pins added). `internal/vm/runtime_v2_net_waiter_contract_test.go`:
  249 -> 257 (contract update, below).
- Untouched by the implementation pass: `Makefile`/CI (gate test names
  unchanged), `rt_async_trace.c` (no new counters), `rt_runtime.c`, and the
  four fd contract tests. The main-session closeout later lowered
  `.loc-legacy-allowlist` ceilings to the new actual line counts.

Design record (Global Rule 2 answers):

- Snapshot semantics preserved: the poll set is copied from registry rows
  into the shard scratch under `ex->lock` before the unlock;
  `poll()` and completion after relock read only that copy, so rows
  attached/detached/swap-removed by other workers during an in-flight poll
  cannot change the cycle. Stale snapshot completions are benign no-ops:
  `rt_executor_wake_net_waiters_for_key` on a key with no waiters returns
  `{0,0}` and `fd_registry_bridge_net_detach_if_last` early-returns on
  `removed==0`. New mid-poll interest still wakes the poller through the
  unchanged `park_current` wake-pipe kick. Completion fan-out is unchanged:
  read-ready completes `net_read_key(fd)` + `net_accept_key(fd)`,
  write-ready completes `net_write_key(fd)`; the poll-error path still
  completes every snapshot key.
- fd-registry-attach-miss RESOLVED (named bridge from Task 6): after
  `prepare_park`, `net_wait_current_task` verifies
  `rt_fd_registry_net_interest_present`; on a miss (attach allocation
  failure) it undoes the park under the same `ex->lock` hold
  (`remove_waiter`, clear `park_prepared`/`park_key`/`pending_key`) and
  returns spurious readiness — the nonblocking net op returns WouldBlock and
  the caller re-waits. Coverage is total: net keys reach `pending_key` only
  from `net_wait_current_task` (grep-verified), and any pre-park removal of
  the verified waiter sets the wake token, so `park_current` refuses to park.
  A parked net waiter therefore always has a registry row. Under persistent
  OOM this degrades to a retry loop, strictly better than the adjacent
  legacy `panic_msg` boundary in `ensure_waiter_cap`.
- Why the unresolved bridge would have been a live defect (poll-caller
  trace with `net_len>0` and zero rows): `next_ready` would return 0 without
  sleeping (`net_polling` already cleared) -> spurious `"async deadlock"`
  panic in `run_until_done`; `rt_worker_main` would fall to
  `pthread_cond_wait(ready_cv)` with the fd never polled (lost wakeup);
  `rt_io_main` would loop begin/instant-0/continue forever (busy spin). The
  resolution makes the state unreachable, so no new spin or missed sleep is
  introduced.
- Capacity and gating invariant: poll capacity comes from
  `rt_fd_registry_len`; `begin_net_poll`/`has_net_waiters` stay
  waiter-derived (`store->net_len`). Invariant relied on: a row exists iff a
  parked net waiter for that fd exists (Task 6 row lifetime + this task's
  attach-miss resolution), so `net_len>0` implies rows exist. The
  `SURGE_ASYNC_DEBUG` bridge recount continues to police stale interest,
  which after this task would be the only route to a level-triggered io-loop
  spin (recorded as a Task 8 fixture target in NOTES).

Deletion evidence (RULES: references, build, tests, static pins):

- Pre-deletion `rg -n` inventories recorded for
  `rt_executor_visit_net_waiters` (4 hits: impl, decl, sole caller
  `rt_net.c:928`, static pin), `rt_waiter_key_visitor` (4),
  `rt_executor_net_waiter_len` (4: impl, decl, sole caller `rt_net.c:917`,
  static pin), `rt_executor_waiter_len` (4: impl, decl, sole caller
  `rt_net.c:926` scan-entries increment, static pin), `collect_net_poll_fd`
  (2), `NetPollBuildContext` (4), `NetPollFd` (12).
- Zero-net-producer greps re-run immediately before deleting the
  `wake_key_all_with_policy` net branch: `rg -n 'wake_key_all'
  runtime/native` -> producers `blocking_key` (`rt_async_blocking.c:110`),
  `scope_key` (`rt_async_state.c:1248`), `join_key`
  (`rt_async_state.c:1371`); `rg -n 'add_wait_key\(' runtime/native` -> all
  producers in `rt_async_task.c` build join/timer/channel keys only.
- Post-deletion `rg -n` over `*.c *.h *.go`: zero hits for all seven
  symbols. Build and tests green (checks below).
- `runtime_v2_waiter_static_test.go` pin list before: ensure_cap,
  `rt_executor_waiter_len`, `rt_executor_net_waiter_len`,
  `rt_executor_visit_net_waiters`, wake_net_waiters_for_key,
  ensure_waiter_cap, remove/add_waiter, clear_wait_keys, add_wait_key,
  prepare_park, pop_waiter, 4 store accessors, 11 key constructors. After:
  identical minus the three deleted-symbol pins. Recorded as a deliberate
  default-tag gate contract change (main-agent approved).

CI-gate contract update (`TestRuntimeV2NetWaiterTraceContract`, runs inside
`make runtime-v2-check`; main-agent approved):

- All 18 `TRACE_NET` fields keep presence assertions (dump format unchanged;
  Task 12 owns counter naming — nothing renamed, only increment sites of
  `io_waiter_scan_entries`, `io_waiter_net_entries`, and
  `io_poll_dedup_checks` were deleted with the legacy build).
- `io_waiter_scan_entries` and `io_waiter_net_entries` moved out of the
  non-zero list; the test now asserts `io_waiter_scan_entries==0`,
  `io_waiter_net_entries==0`, and `io_poll_dedup_checks==0`. These zeros are
  the machine-checkable "legacy waiter-derived rebuild path unused"
  acceptance evidence for the epic. The `net_entries <= scan_entries`
  relation is superseded by the zero assertions;
  `io_poll_rebuilds == io_poll_calls` stays.
- The four fd contract tests are byte-identical and 4/4 green;
  `io_poll_waiters_max` keeps its meaning (max distinct fd rows per poll
  build: old deduped-fd count == new registry-row count).

Checks (all run by the executor, in order):

- BEFORE benchmark as the first action, pre-edit: scratch compiler
  `go build -ldflags "$(./scripts/ldflags.sh --local)"` in the session
  scratchpad; `version --full` pin `617f8cfa5881` == `git rev-parse
  --short=12 HEAD`; `timeout 120s env SURGE=<scratch>/surge
  ./scripts/bench_native_net.sh` ->
  `build/benchmarks/runtime-v2-task07-native-net-before.md` (24 rows);
  leftover-process `ps` check clean.
- Pre-deletion inventories and producer greps (above).
- Post-edit: `clang-format --dry-run --Werror` on all six touched C/H files
  clean (one violation found and fixed in `rt_async_waiter.c`);
  `gofmt -l internal/vm` clean; `go vet -tags runtime_v2_pending
  ./internal/vm` clean; post-deletion no-hit greps.
- `make c-check`: passed. `make cppcheck`: passed, 32/32 files, no
  suppressions.
- Static gates: `TestRuntimeV2FDRegistryStaticShape` PASS (0.10s, now
  pinning `rt_fd_poll_interest` + the two Task 7 reads);
  `TestRuntimeV2FDRegistryStaticBoundary` PASS (0.03s, zero edits);
  `TestRuntimeV2WaiterHelperStaticBoundary` PASS (0.03s, post pin removal).
- `make runtime-v2-check`: passed (MT seed green this run, no flake; tagged
  waiter gate 12/12 including the updated
  `TestRuntimeV2NetWaiterTraceContract` PASS 2.45s).
- `make check`: `exit=0` including `check_file_sizes.sh` (`rt_net.c` 1002
  LEGACY OK <=1024).
- Task 3 tagged contract 4-pack, verbatim Task 3 command -> 4/4 PASS
  (16.0s).
- Focused probes: `TestMTNetWaiterWakeupLatency` PASS (2.31s);
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer` PASS (4.34s).
- Debug-path proof: `SURGE_ASYNC_DEBUG=1` rerun of
  `RepeatedReadinessSingleFD` + `DuplicateReadWaitersBothComplete` -> 2/2
  PASS with the registry live as the sole poll input.
- Default-tag proof: `go test ./internal/vm -run '^TestRuntimeV2' -count=1`
  -> ok (only the default-tag static boundary runs).
- AFTER benchmark: rebuilt scratch compiler from the modified tree (version
  pin `617f8cfa5881` recorded together with `git diff --stat`, 9 files
  +163/-127, per the dirty-tree evidence rule) ->
  `build/benchmarks/runtime-v2-task07-native-net-after.md` (24 rows);
  leftover-process `ps` checks clean after both runs.
- `git diff --check`: clean.
- Skipped per approved plan: Sentrux scans and the commit (main session owns
  both).

Benchmark before/after (echo rows, us/op avg; full 24-row reports under
ignored `build/benchmarks/`):

| row | before | after |
| --- | ---: | ---: |
| 1/echo/seq | 65.38 | 62.14 |
| 1/echo/pipe | 25.66 | 23.61 |
| 2/echo/seq | 88.32 | 78.98 |
| 2/echo/pipe | 39.76 | 39.51 |
| 4/echo/seq | 85.99 | 85.71 |
| 4/echo/pipe | 39.52 | 37.27 |
| 8/echo/seq | 83.63 | 82.00 |
| 8/echo/pipe | 39.63 | 37.33 |

Trace counters (1/echo/seq exemplar; pattern holds across all 24 rows):
`io_waiter_scan_entries` 13094 -> 0, `io_waiter_net_entries` 4366 -> 0,
`io_poll_dedup_checks` 0 -> 0, `io_poll_rebuilds` == `io_poll_calls` on both
sides (4366/4366 -> 4164/4164), `io_poll_allocs` 2 -> 2 (flat),
`io_direct_waits`/`io_waiter_completed` equivalent (1775 -> 1799, workload
noise). Expected movements all confirmed; latency flat or better on every
echo row.

Variance note (recorded, not a regression): the AFTER report shows
`1/manager/seq` 129.62 vs 109.11 before. A same-binary variance re-run
(`SURGE_NET_BENCH_THREADS="1 2" SURGE_NET_BENCH_MODES=manager`, scratch
report) reproduced 110.86 us/op for that row and 173.41 for `2/manager/seq`
(before 170.31), with poll counters flat — run-to-run scheduling variance of
the channel-hop mode, not a poll-path change. No unexplained regression
remains.

Main-session closeout evidence:

- Sentrux MCP scans and rule checks passed for all mandatory roots:
  repository root `quality_signal=6198`, runtime `5228`, and runtime/native
  `5172`.
- `sentrux gate .`: passed, `6198 -> 6198`, no degradation.
- `sentrux gate runtime`: passed, `5195 -> 5228`, no degradation.
- `sentrux gate runtime/native`: passed, `5159 -> 5172`, no degradation.
- `git diff --check`: clean.
- `make c-check`: passed.
- `make cppcheck`: passed, 32/32 files.
- `make runtime-v2-check`: passed.
- `make check`: passed; `check_file_sizes.sh` checked the six dirty C/H files
  and reported 4 OK plus 2 legacy-ceiling files.
- Static gates passed:
  `TestRuntimeV2FDRegistryStaticShape`,
  `TestRuntimeV2FDRegistryStaticBoundary`, and
  `TestRuntimeV2WaiterHelperStaticBoundary`.
- Task 3 fd-registry contract 4-pack passed under
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0`.
- `TestRuntimeV2NetWaiterTraceContract` passed and asserts
  `io_waiter_scan_entries==0`, `io_waiter_net_entries==0`, and
  `io_poll_dedup_checks==0`.
- Focused net probes passed: `TestMTNetWaiterWakeupLatency` and
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer`.
- Debug-path proof passed with `SURGE_ASYNC_DEBUG=1` for
  `RepeatedReadinessSingleFD` and `DuplicateReadWaitersBothComplete`.
- Fresh closeout native net benchmark passed with report
  `build/benchmarks/runtime-v2-task07-closeout-native-net.md`; exemplar
  `1/echo/seq` row was `64.66 us/op` with `waiter scan entries=0`,
  `net waiter entries=0`, `dedup checks=0`, `poll rebuilds=4431`,
  `net poll calls=4431`, and `poll allocs=2`.
- Clean leftover-process check:
  `pgrep -af '[b]ench_native_net|[n]et_request_reply' || true` printed no
  benchmark process.
- Sentrux gate baselines
  `.sentrux/baseline.json`, `runtime/.sentrux/baseline.json`, and
  `runtime/native/.sentrux/baseline.json` are committed with this task so
  future `sentrux gate` checks are reproducible in a clean checkout.
- `.loc-legacy-allowlist` ceilings were lowered to the new actual line counts:
  `rt_async_state.c` `1731 -> 1727` and `rt_net.c` `1024 -> 1002`.

## Task 8 Evidence: Close, Cancel, And Re-register Tests

Date: 2026-07-02. Test-writing and docs only; no runtime/native, Makefile, CI,
or existing Task 7 code changes. New file:
`internal/vm/runtime_v2_fd_registry_lifecycle_test.go` (297 lines,
`//go:build runtime_v2_pending`, package `vm_test`). The existing 499-line fd
contract file was left unchanged.

Green cancel/re-register proofs added:

- `TestRuntimeV2FDRegistryCancelledDuplicateReadWaiterPreservesLiveAndReregister`
  parks duplicate read waiters on one fd, cancels one, proves the remaining
  read waiter completes, then parks and completes a new read waiter on the same
  fd.
- `TestRuntimeV2FDRegistryCancelledReadInterestPreservesWriteInterest` parks a
  read waiter and a bulk write waiter on the same fd, cancels the read waiter,
  and proves the write interest still drains to completion.

Expected-red close proofs added for Task 9:

- `TestRuntimeV2FDRegistryCloseWakesParkedAcceptWaiter` parks an accept waiter,
  closes the listener, and expects the waiter to complete with a synchronous
  net error. Current behavior: exit status `3`, stdout
  `accept_close_timeout`; trace keeps `io_direct_waits=1`,
  `io_waiter_completed=0`,
  `io_waiter_scan_entries=0`, `io_waiter_net_entries=0`,
  `io_poll_dedup_checks=0`.
- `TestRuntimeV2FDRegistryCloseWakesParkedReadWaiter` parks a read waiter,
  closes the connection while the peer stays open, and expects the waiter to
  complete with a synchronous net error. Current behavior: exit status `3`,
  stdout `read_close_timeout`; trace keeps `io_direct_waits=2`,
  `io_waiter_completed=1`,
  `io_waiter_scan_entries=0`, `io_waiter_net_entries=0`,
  `io_poll_dedup_checks=0`.

Numeric fd reuse was not added as a Go-only fixture. The allowed Task 8 write
set excludes a native helper, and the Go/socket surface cannot force numeric fd
reuse deterministically enough for CI. Task 9 must supply generation or
closed-state stale-wake proof, or expand scope for a deterministic helper.

Checks:

- `gofmt -l internal/vm/runtime_v2_fd_registry_lifecycle_test.go`: clean;
  `wc -l` reported `297`.
- `go vet -tags runtime_v2_pending ./internal/vm`: passed.
- `go test ./internal/vm -run '^TestRuntimeV2FDRegistry' -count=1 --timeout
  60s`: passed with `[no tests to run]`.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistry(CancelledDuplicateReadWaiterPreservesLiveAndReregister|CancelledReadInterestPreservesWriteInterest)$'
  -count=1 -parallel=1 -p=1 -v --timeout 120s`: passed, 2/2 tests,
  package time `12.464s`.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistry(CloseWakesParkedAcceptWaiter|CloseWakesParkedReadWaiter)$'
  -count=1 -parallel=1 -p=1 -v --timeout 120s`: expected-red, build clean,
  failed only through current runtime behavior (`accept_close_timeout` and
  `read_close_timeout`).
- Main-session reproduction of the same expected-red command also failed only
  through those two runtime timeouts; poll call counts varied by run, while the
  legacy waiter-scan counters stayed zero.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v --timeout
  90s`: passed, `TestMTNetWaiterWakeupLatency` `2.49s`, package time
  `2.499s`.
- `make runtime-v2-check`: passed; the stable Runtime V2 gate does not include
  the expected-red close fixtures yet.
- New-file whitespace check with `git diff --no-index --check`: clean for
  `internal/vm/runtime_v2_fd_registry_lifecycle_test.go` before staging.
- `check_file_sizes.sh`: passed with no applicable changed C/H files.
- Review subagent found no P0/P1 blockers; residual risks were the documented
  numeric-fd-reuse gap and short park-window sleeps matching nearby tests.
- `sentrux gate .`: passed, `6198 -> 6198`, no degradation.
- `sentrux gate runtime`: passed, `5195 -> 5228`, no degradation.
- `sentrux gate runtime/native`: passed, `5159 -> 5172`, no degradation.
- `git diff --check`: passed.

## Task 9 Evidence: Close, Cancel, And Re-register Migration

Date: 2026-07-02. Runtime migration completed in the fd registry/net paths and
static behavior proof extended. Touched runtime files:
`runtime/native/rt_fd_registry.h`, `runtime/native/rt_fd_registry.c`, and
`runtime/native/rt_net.c`; touched proof file:
`internal/vm/runtime_v2_fd_registry_static_test.go`.

Behavior implemented:

- fd registry rows now use a durable monotonic `next_generation`; row
  remove/recreate does not reset generation, and generation exhaustion returns
  `RT_RUNTIME_STATUS_ALLOCATION_FAILED`.
- `rt_fd_registry_mark_closed` records a compact close lifecycle snapshot
  (`fd`, `generation`, `want_accept`, `want_read`, `want_write`) without
  allocation and marks the row closed. Missing rows return OK with an empty
  snapshot; invalid arguments return an explicit status.
- Closed rows are excluded from poll snapshots and from
  `rt_fd_registry_net_interest_present`.
- Poll snapshots carry generation and exact accept/read/write interests.
  Completion fan-out now goes through registry helpers that use
  `rt_fd_registry_completion_state`, so stale fd/generation/open-state/current
  interest mismatches do not wake waiters.
- close paths validate the handle, capture fd, mark the registry closed under
  `ex->lock`, raw-close the fd outside the executor lock, then wake only the
  close snapshot keys and signal the net poll/`io_cv` sleepers.
- `POLLNVAL` is treated as readiness/error in both immediate readiness probes
  and poll completion, which makes resumed close waiters observe a net error
  instead of parking again on the closed fd.

Task 8 expected-red close tests are now green:

- `TestRuntimeV2FDRegistryCloseWakesParkedAcceptWaiter`: passed after Task 9;
  the prior expected-red stdout was `accept_close_timeout`.
- `TestRuntimeV2FDRegistryCloseWakesParkedReadWaiter`: passed after Task 9;
  the prior expected-red stdout was `read_close_timeout`.
- Existing green cancellation/re-register tests also stayed green:
  `CancelledDuplicateReadWaiterPreservesLiveAndReregister` and
  `CancelledReadInterestPreservesWriteInterest`.

Deterministic stale poll snapshot proof:

- `TestRuntimeV2FDRegistryGenerationStaleSnapshotProof` compiles and runs a
  tiny C program against `rt_fd_registry.c` with local stubs. It proves fd `42`
  snapshot generation `1` becomes stale after mark-closed plus detach, and a
  recreated fd `42` row gets generation `2` while the old snapshot remains
  stale. The same proof checks explicit generation-overflow status.
- This proof does not rely on OS fd allocation or numeric fd reuse luck.
- Boundary: Task 9 protects stale poll snapshots and registry-routed waiter
  completions. It does not make copied `TcpConn`/`TcpListener` handles
  generation-aware; that remaining fd-reuse hazard is tracked as
  RV2-DEBT-010.

Trace contract:

- `TestRuntimeV2NetWaiterTraceContract` passed with the `runtime_v2_pending`
  tag and continues to assert
  `io_waiter_scan_entries==0`, `io_waiter_net_entries==0`, and
  `io_poll_dedup_checks==0`.
- Poll input remains fd-registry snapshots only; no waiter-store scan was
  reintroduced.

Checks:

- `gofmt -w internal/vm/runtime_v2_fd_registry_static_test.go`: passed.
- `go test -tags runtime_v2_pending ./internal/vm -run
  'TestRuntimeV2FDRegistry(StaticShape|GenerationStaleSnapshotProof)$'
  -count=1 -v --timeout 60s`: passed, package time `0.165s`.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  'TestRuntimeV2FDRegistry(CancelledDuplicateReadWaiterPreservesLiveAndReregister|CancelledReadInterestPreservesWriteInterest|CloseWakesParkedAcceptWaiter|CloseWakesParkedReadWaiter)$'
  -count=1 -v --timeout 90s`: passed, package time `16.013s`.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2NetWaiterTraceContract$' -count=1 -v --timeout 90s`:
  passed, package time `2.539s`.
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -count=1 -v --timeout 90s`: passed,
  package time `2.541s`.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestNativeNetSingleThreadBlockingChannelInAsyncServer$' -count=1 -v
  --timeout 90s`: passed, package time `4.453s`.
- `make c-check`: passed.
- `make cppcheck`: passed.
- `make runtime-v2-check`: first run failed once on
  `TestMTChannelParkUnpark` timeout; isolated
  `TestMTChannelParkUnpark` rerun passed, and full `make runtime-v2-check`
  rerun passed.
- `make check`: passed; file-size gate reports `rt_net.c` as
  `LEGACY OK <=1002`, with `rt_fd_registry.c` `366` lines and
  `rt_fd_registry.h` `111` lines.
- `git diff --check`: passed.
- Main-session review subagent found no P0/P1/P2/P3 blockers. Residual
  boundaries are RV2-DEBT-010 and the already tracked Task 12 trace cleanup.
- Main-session Sentrux gates passed without degradation: root `6198 -> 6195`,
  `runtime` `5195 -> 5243`, and `runtime/native` `5159 -> 5188`.

## Draft Creation Evidence

- `git diff --check`: passed with empty output after creating the Epic 4
  document set.
- Sentrux repository scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6198`, `files=4775`, `import_edges=1890`, and
  `lines=377913`.
- Sentrux rules were added after draft creation. Root `sentrux check .` passes
  with quality `6198`.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5195`, `files=35`, `import_edges=33`, and `lines=15275`.
- Runtime `sentrux check runtime` passes with quality `5195`.
- Sentrux runtime/native scan for
  `/home/zov/projects/surge/surge/runtime/native` returned
  `quality_signal=5159`, `files=34`, `import_edges=33`, and `lines=15260`.
- Runtime/native `sentrux check runtime/native` passes with quality `5159`.
- MCP `check_rules` also passes for root, `runtime/`, and `runtime/native`.
