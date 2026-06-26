# Epic 3 Evidence

This file records task evidence for
`03-owner-local-waiters-and-runtime-refactor.md`.

## Starting State

Epic 3 is drafted but not implemented.

Known starting facts:

- Epic 2 completed the `N=1` `rt_runtime` / `rt_shard` skeleton.
- Waiters still use executor-global storage.
- The broad VM/native/LLVM regex remains accepted backend-test debt.
- Missing Sentrux rules remain debt, not compliance.
- The largest relevant native files at draft time are:
  - `runtime/native/rt_async_state.c`: 2431 lines;
  - `runtime/native/rt_term.c`: 1091 lines;
  - `runtime/native/rt_net.c`: 1040 lines;
  - `runtime/native/rt_fs.c`: 978 lines;
  - `runtime/native/rt_async_task.c`: 768 lines;
  - `runtime/native/rt_async_channel.c`: 549 lines;
  - `runtime/native/rt_async_internal.h`: 460 lines.
- Initial dead-code seed: `rt_select_poll_tasks` is suspect only and must not
  be removed without generated-IR search, ABI review, focused tests, and
  Sentrux evidence.

## Task Evidence Ledger

Add one section per closed task. Use `EVIDENCE_TEMPLATE.md` for runtime-code
tasks and record exact commands, Sentrux paths, line-count changes, and known
debt.

## Task 01: Kickoff Baseline And Sentrux

Status: complete.

Branch and commit:

- Branch: `codex/runtime-net-scheduler-refactor`, ahead of origin by one commit
  at task start.
- Start commit: `f4f83c4d docs(runtime): draft Runtime V2 waiter epic`.
- Working tree: clean before implementation.

Runtime/native line-count baseline:

- `runtime/native/rt_async_state.c`: 2431 lines.
- `runtime/native/rt_term.c`: 1091 lines.
- `runtime/native/rt_net.c`: 1040 lines.
- `runtime/native/rt_fs.c`: 978 lines.
- `runtime/native/rt_async_task.c`: 768 lines.
- `runtime/native/rt_string.c`: 762 lines.
- `runtime/native/rt_bignum_int.c`: 744 lines.
- `runtime/native/rt_bignum_uint_div.c`: 718 lines.
- `runtime/native/rt_bignum_float_core.c`: 654 lines.
- `runtime/native/rt_bignum_api.c`: 640 lines.
- `runtime/native/rt_async_channel.c`: 549 lines.
- `runtime/native/rt_bignum_format.c`: 501 lines.
- `runtime/native/rt_async_internal.h`: 460 lines.

Startup gates:

- `make runtime-v2-check`: passed.
- `make c-check`: passed.
- `make cppcheck`: passed.
- `make check`: passed.

Sentrux baseline:

- Root scan `/home/zov/projects/surge/surge`: `quality_signal=6207`,
  bottleneck `modularity`.
- Runtime scan `/home/zov/projects/surge/surge/runtime`:
  `quality_signal=5209`, bottleneck `redundancy`.
- Native scan `/home/zov/projects/surge/surge/runtime/native`:
  `quality_signal=5172`, bottleneck `redundancy`.
- `session_start` saved the native scan baseline at `quality_signal=5172`.
- `check_rules`: missing `.sentrux/rules.toml` for root, runtime, and
  runtime/native. This remains debt, not compliance.

Accepted debt confirmed:

- Do not use `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required
  green gate in Epic 3.
- Missing Sentrux rules are recorded honestly and cannot be treated as a
  passing rule gate.

## Draft Creation Evidence

- Docs created for Epic 3 scope and brief task list.
- `git diff --check`: passed.
- Sentrux root scan: `/home/zov/projects/surge/surge`, `quality_signal=6207`.
- Sentrux runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5209`.
- `check_rules`: missing `.sentrux/rules.toml` for both scanned paths. This is
  debt, not rule compliance.
