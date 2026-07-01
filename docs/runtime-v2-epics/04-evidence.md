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
- Missing Sentrux rule files remain debt, not compliance.

## Task Evidence Ledger

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Pending | Kickoff baseline, Sentrux state, line counts, and gate plan. |
| 2 | Pending | FD registry dependency map. |
| 3 | Pending | FD lifecycle behavior contract tests. |
| 4 | Pending | Registry static shape tests. |
| 5 | Pending | Registry container skeleton. |
| 6 | Pending | Net wait registration migration. |
| 7 | Pending | Poll-from-registry migration. |
| 8 | Pending | Close/cancel/re-register behavior tests. |
| 9 | Pending | Close/cancel/re-register migration. |
| 10 | Pending | Wake-fd and shutdown behavior tests. |
| 11 | Pending | Wake-fd and shutdown migration. |
| 12 | Pending | Trace counters and benchmark contract. |
| 13 | Pending | CI gate wiring. |
| 14 | Pending | Large-file refactor tranche. |
| 15 | Pending | Closeout gates and handoff. |

## Draft Creation Evidence

- `git diff --check`: passed with empty output after creating the Epic 4
  document set.
- Sentrux repository scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6198`, `files=4775`, `import_edges=1890`, and
  `lines=377913`.
- Sentrux `check_rules` for `/home/zov/projects/surge/surge` still reports
  missing `.sentrux/rules.toml`; this is debt, not compliance.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5195`, `files=35`, `import_edges=33`, and `lines=15275`.
- Sentrux `check_rules` for `/home/zov/projects/surge/surge/runtime` still
  reports missing `runtime/.sentrux/rules.toml`; this is debt, not compliance.
- Sentrux runtime/native scan for
  `/home/zov/projects/surge/surge/runtime/native` returned
  `quality_signal=5159`, `files=34`, `import_edges=33`, and `lines=15260`.
- Sentrux `check_rules` for `/home/zov/projects/surge/surge/runtime/native`
  still reports missing `runtime/native/.sentrux/rules.toml`; this is debt, not
  compliance.
