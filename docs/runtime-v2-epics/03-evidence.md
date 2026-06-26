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

## Draft Creation Evidence

- Docs created for Epic 3 scope and brief task list.
- `git diff --check`: passed.
- Sentrux root scan: `/home/zov/projects/surge/surge`, `quality_signal=6207`.
- Sentrux runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5209`.
- `check_rules`: missing `.sentrux/rules.toml` for both scanned paths. This is
  debt, not rule compliance.
