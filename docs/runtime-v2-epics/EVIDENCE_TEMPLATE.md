# Runtime V2 Evidence Template

Copy this section into a Runtime V2 task or epic before closeout. Fill every
row. If a row does not apply, write `N/A` and the exact reason.

Keep this evidence about observed behavior, ownership, wakeups, checks,
counters, reports, and recovery. Synchronization design belongs in the task that
introduces it, not in this reusable template.

## Template

### Task Identity And Scope

- Task:
- Epic:
- Date:
- Author/session:
- Scope:
- Out of scope:
- Proving spike: `no` / `yes`

If this is a proving spike, fill these fields before implementation:

- Hypothesis:
- Files and surfaces allowed to change:
- Behavior that is explicitly non-final:
- Proof test, benchmark, trace, invariant, or negative compile test:
- Success criteria:
- Failure criteria:
- Rollback note:

### Baseline Commit/Status

- Baseline commit:
- Branch/worktree:
- Status before:
- Status after:
- Dirty or untracked files not touched:
- Local environment blockers:

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
|  |  |  |  |

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
|  |  |  |  |

### Sentrux Root/Scoped Signals

| Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | --- | --- | --- |
| Repository |  | Before |  |  |  |
| Scoped |  | Before |  |  |  |
| Scoped |  | After |  |  |  |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
| --- | --- | --- | --- | --- |
|  |  |  |  |  |

### Benchmarks And Generated Reports

| Benchmark | Expected baseline | Actual key rows | Generated report path | Notes |
| --- | --- | --- | --- | --- |
|  |  |  |  |  |

### Trace Counters/Liveness Proof

| Probe or counter | Expected result | Actual result | Evidence path | Pass/blocker |
| --- | --- | --- | --- | --- |
|  |  |  |  |  |

### Known Regressions

- None known, or list each regression with impact, proof, and blocker status.

### Dead Ends / Paths Not To Retry

- None, or list each path with the failed hypothesis and evidence.

### Rollback/Recovery Notes

- Files or changes to revert:
- Generated artifacts to remove:
- Runtime processes, sockets, or temporary state to clean up:
- Recovery command or owner:

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
|  |  |  |  |

### Notes Consolidation Checklist

- [ ] `NOTES.md` has the start context and intended proof.
- [ ] `NOTES.md` has what changed, what was checked, and what was skipped.
- [ ] Durable decisions moved from `NOTES.md` into the owning epic, `RULES.md`,
  `docs/RUNTIME_V2.md`, or another linked document.
- [ ] Skipped checks record the exact reason and blocker status.
- [ ] Dead ends are recorded so future tasks do not retry them.
- [ ] Follow-ups and blockers have an owner, target document, or next task.

## Examples

### Docs-Only Task Example

- Task: Epic 1 Task 4, evidence template.
- Scope: create `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`; no runtime,
  compiler, ABI, benchmark, or task-status changes.
- Baseline: `git rev-parse HEAD` returned `<commit>`;
  `git status --short` showed the docs directory as untracked before this
  task.
- Files touched: `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`, created.
- Contracts touched: `N/A`; documentation format only.
- Sentrux: `N/A` for this docs-only task; record the active epic-level
  repository/scoped signals if available.
- Commands/checks:

  | Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
  | --- | --- | --- | --- | --- |
  | `git diff --check` | no output | no output | `0` | tracked-file whitespace gate |
  | `git diff --no-index --check /dev/null docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md` | no whitespace output | no output | `1` expected for different files | new untracked file check |

- Benchmarks/reports: `N/A`; no runtime path changed.
- Trace/liveness: `N/A`; no runtime path changed.
- Known regressions: none known.
- Dead ends: none.
- Rollback/recovery: remove `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`.
- Follow-ups/blockers: parent integration still needs links from shared docs.

### Runtime-Code Task Example

- Task: Epic 2 Task N, runtime wakeup ownership change.
- Scope: change the named native runtime path; preserve source-visible async
  behavior, VM/native semantic parity under native `threads=1`, and task
  liveness.
- Baseline: `git rev-parse HEAD` returned `<commit>`;
  `git status --short` returned `<exact output>`.
- Files touched:

  | Path | Change | Reason | Size/limit note |
  | --- | --- | --- | --- |
  | `runtime/native/<file>.c` | update runtime ownership path | keep wakeup owner explicit | over 500 lines; line count stayed flat |

- Contracts touched:

  | Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
  | --- | --- | --- | --- |
  | task is not polled concurrently | `docs/CONCURRENCY.md`, `docs/RUNTIME.md` | preserved | MT/async tests passed |
  | channel FIFO | `docs/LANGUAGE.md`, Epic 1 contract table | preserved | channel tests and trace counters passed |

- Sentrux:

  | Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
  | --- | --- | --- | --- | --- | --- |
  | Repository | `/home/zov/projects/surge/surge` | Before | `<n>` | `<summary>` | `session_start=<id>` |
  | Scoped | `/home/zov/projects/surge/surge/runtime` | Before | `<n>` | `<summary>` | rules present or blocker recorded |
  | Scoped | `/home/zov/projects/surge/surge/runtime` | After | `<n>` | `<summary>` | `health`, `check_rules`, `session_end` recorded |

- Commands/checks:

  | Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
  | --- | --- | --- | --- | --- |
  | `git diff --check` | no output | no output | `0` | whitespace gate |
  | `make c-check` | pass | pass | `0` | native C checks |
  | `make cppcheck` | pass | pass | `0` | static checks |
  | `go test ./internal/vm -run 'MT|Async|Net|LLVM'` | accepted baseline debt unless this task owns the test-matrix rewrite | fails only with recorded debt classes, or passes after debt is fixed | `0` or recorded nonzero | link `01-baseline-evidence.md`; separate any new failure class |
  | `make check` | pass | pass | `0` | full project gate |

- Benchmarks/reports:

  | Benchmark | Expected baseline | Actual key rows | Generated report path | Notes |
  | --- | --- | --- | --- | --- |
  | `./scripts/bench_native_net.sh` | no material regression vs baseline | `<rows>` | `<path>` | timeout wrapper used |
  | `./scripts/bench_native_channels.sh` | no material regression vs baseline | `<rows>` | `<path>` | report committed or linked |

- Trace/liveness:

  | Probe or counter | Expected result | Actual result | Evidence path | Pass/blocker |
  | --- | --- | --- | --- | --- |
  | SIGUSR1 live snapshot | runnable work drains | `<summary>` | `<path>` | pass |
  | net wakeups | wakeups increase while sockets progress | `<counters>` | `<path>` | pass |
  | parked-with-work invariant | zero at steady state | `0` | `<path>` | pass |
  | cancellation/shutdown probe | no hang, no lost wakeup | pass | `<path>` | pass |

- Known regressions: none known, or list exact failing command and blocker.
- Dead ends: record any rejected path and why the evidence disproved it.
- Rollback/recovery: revert the touched files, remove generated benchmark
  reports if they are task-local, and stop any leftover benchmark processes.
- Follow-ups/blockers: record missing probes, skipped commands, or open contract
  questions before closing the task.
