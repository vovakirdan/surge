# Epic 2 Evidence

This file records task-by-task evidence for
`02-n1-runtime-shard-structure.md`.

Do not use this file to hide known debt. The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` is accepted backend-test debt
until the later test/backend matrix epic fixes or replaces it. Epic 2 evidence
must separate that debt from new runtime regressions.

## Task Evidence Index

| Task | Evidence status | Notes |
| --- | --- | --- |
| 1. Kickoff Evidence | Complete | Baseline, accepted VM debt, and Sentrux missing-rules deferral recorded. |
| 2. Field Ownership Map | Pending | Link the ownership map and deferred field groups. |
| 3. Runtime V2 Test And CI Contract | Pending | Define stable local and CI probes. |
| 4. Runtime/Shard Skeleton Tests | Pending | Record failing or selected skeleton checks. |
| 5. Runtime/Shard Skeleton | Pending | Record implementation checks and Sentrux deltas. |
| 6. Scheduler Shape Tests | Pending | Record selected scheduler liveness checks. |
| 7. Scheduler Shape Migration | Pending | Record scheduler migration checks and traces. |
| 8. Net Poll Scratch Tests | Pending | Record net wake and benchmark baseline. |
| 9. Net Poll Scratch Migration | Pending | Record net migration checks and benchmark rows. |
| 10. Channel/Blocking Compatibility Tests | Pending | Record channel and fallback checks. |
| 11. Channel/Blocking Compatibility Migration | Pending | Record migration checks and trace rows. |
| 12. CI Runtime V2 Gates | Pending | Record new CI target/job and local result. |
| 13. Accessor Cleanup And Static Gates | Pending | Record static checks and quality deltas. |
| 14. Epic Closeout | Pending | Record final gates and handoff to Epic 3. |

## Task 1: Kickoff Evidence

### Task Identity And Scope

- Task: Epic 2 Task 1, Kickoff Evidence.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: record the current checkout, accepted VM/backend-test debt, Sentrux
  root/runtime state, and the first docs-only gate before runtime code changes.
- Out of scope: runtime implementation edits, Sentrux rule-file creation,
  benchmarks, liveness probes, broad focused VM regex runs, staging, and commit.
- Proving spike: `no`.

### Baseline Commit/Status

- Baseline commit: `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`.
- Branch/worktree: `codex/runtime-net-scheduler-refactor`; clean before this
  task.
- Status before: `git status --short` produced empty output.
- Status after closeout: these docs are committed by the main-agent Task 1
  closeout commit, and `git status --short` is checked immediately after that
  commit.
- Dirty or untracked files not touched: none observed before this task.
- Local environment blockers: none observed. Sentrux rules are missing at both
  scan roots; this is recorded as a rule-compliance blocker/deferral, not as a
  passing rule gate.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-evidence.md` | Added this Task 1 evidence section and marked the index row complete. | Keep Epic 2 evidence current before runtime code changes. | Documentation only; runtime file-size limits do not apply. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 1 kickoff handoff. | Make the next task startable without chat context. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording to point at kickoff evidence. | Reflect that the kickoff evidence step is recorded. | Documentation only. |

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
| Runtime behavior | Epic 2 scope and preserved behavior boundary | N/A | Docs-only task; no runtime files changed. |
| Public native ABI | Epic 2 acceptance gates | N/A | Docs-only task; no native ABI files changed. |
| Focused VM debt handling | `01-baseline-evidence.md`, `OPEN_DECISIONS_BEFORE_EPIC_2.md` | Preserved | Broad focused VM regex was not run and remains accepted backend-test debt. |
| Sentrux rule compliance | `SENTRUX_POLICY.md` | Preserved as blocker/deferral | `check_rules` still reports missing rule files at both active scan paths. |

### Accepted VM Debt

`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted
backend-test debt for Epic 2 kickoff. It fails in the current checkout when
timeout-sensitive tests are not skipped; Epic 2 may start without fixing it.
Do not attribute new runtime failures to this debt unless they match the
recorded classes in `01-baseline-evidence.md`. A later test/backend matrix epic
owns the rewrite.

### Sentrux Root/Scoped Signals

| Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | --- | --- | --- |
| Repository | `/home/zov/projects/surge/surge` | Before docs edit | `6210` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4696`, modularity `3435`, redundancy `8588`; cross-module edges `1820`; files `4740`; import edges `1887`; lines `370800` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Scoped | `/home/zov/projects/surge/surge/runtime` | Before docs edit | `5147` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4735`, modularity `3333`, redundancy `2574`; cross-module edges `0`; files `32`; import edges `30`; lines `14883` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Scoped | `/home/zov/projects/surge/surge/runtime` | After | N/A | N/A | N/A; this docs-only kickoff records start evidence only and does not call `session_start`/`session_end`. First runtime-code task must use a scoped Sentrux session. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
| --- | --- | --- | --- | --- |
| `git rev-parse HEAD` | current commit | `e7d9563d5c78a90409e4d6a92bd47d49b30ae830` | `0` | baseline commit |
| `git rev-parse --abbrev-ref HEAD` | current branch | `codex/runtime-net-scheduler-refactor` | `0` | baseline branch |
| `git status --short` | clean before edits | empty output | `0` | baseline status |
| `mcp__sentrux.scan` root | scan root path | `quality_signal=6210`; files `4740`; import edges `1887`; lines `370800` | pass | active path `/home/zov/projects/surge/surge` |
| `mcp__sentrux.health` root | health for root active path | bottleneck `modularity`; root-cause scores recorded above | pass | active path `/home/zov/projects/surge/surge` |
| `mcp__sentrux.check_rules` root | report rule status | missing `/home/zov/projects/surge/surge/.sentrux/rules.toml` | blocker/deferral | missing rules are not compliance |
| `mcp__sentrux.scan` runtime | scan runtime path | `quality_signal=5147`; files `32`; import edges `30`; lines `14883` | pass | active path `/home/zov/projects/surge/surge/runtime` |
| `mcp__sentrux.health` runtime | health for runtime active path | bottleneck `redundancy`; root-cause scores recorded above | pass | active path `/home/zov/projects/surge/surge/runtime` |
| `mcp__sentrux.check_rules` runtime | report rule status | missing `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml` | blocker/deferral | missing rules are not compliance |
| `git diff --check` | no whitespace errors | empty output | `0` | docs-only whitespace gate |
| `make check` | pass | passed in 14.31s; ran `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`, `make c-check`, and `check_file_sizes.sh` | `0` | approved Task 1 gate |

Skipped by explicit Task 1 scope: broad focused VM regex,
standalone `make c-check`, `make cppcheck`, benchmarks, and extra long
liveness probes. `make check` still ran its internal `make c-check` step.

### Benchmarks And Generated Reports

N/A. This task changes documentation only and does not touch a
performance-sensitive runtime path.

### Trace Counters/Liveness Proof

N/A. This task changes documentation only and does not touch scheduler, channel,
net, timer, cancellation, or shutdown behavior.

### Known Regressions

None known. Runtime code was not changed.

### Dead Ends / Paths Not To Retry

- Do not treat missing Sentrux rule files as passing rule checks.
- Do not use the broad focused VM regex as an Epic 2 green gate until the later
  test/backend matrix epic replaces or fixes that debt.

### Rollback/Recovery Notes

- Files or changes to revert: this Task 1 section, the Task 1 index update, the
  Task 1 notes entry, and the Epic 2 status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.
- Recovery command or owner: revert only the three docs touched by this task.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Missing root `.sentrux/rules.toml` | No for this docs-only kickoff; yes for claiming rule compliance. | First runtime-code Epic 2 task or a dedicated Sentrux rules task. | Root `check_rules` cannot pass without a real rules file or an explicit deferral. |
| Missing runtime `.sentrux/rules.toml` | No for this docs-only kickoff; yes for claiming runtime rule compliance. | First runtime-code Epic 2 task or a dedicated Sentrux rules task. | Runtime `check_rules` cannot pass without a real rules file or an explicit deferral. |
| Focused VM regex debt | No for Epic 2 kickoff. | Later test/backend matrix epic. | Existing timeout-sensitive VM/LLVM debt remains accepted baseline debt. |
| Field ownership map | Yes for starting structural runtime movement. | Epic 2 Task 2, `02-tasks/02-field-ownership-map.md`. | The next task must classify current `rt_executor` fields before code movement. |

### Notes Consolidation Checklist

- [x] `NOTES.md` has the start context and intended proof.
- [x] `NOTES.md` has what changed, what was checked, and what was skipped.
- [x] Durable decisions remain in the owning Epic 1 and Epic 2 documents.
- [x] Skipped checks record the exact reason and blocker status.
- [x] Dead ends are recorded so future tasks do not retry them.
- [x] Follow-ups and blockers have an owner, target document, or next task.
