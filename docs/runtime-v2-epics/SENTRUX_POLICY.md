# Runtime V2 Sentrux Policy

This policy records the current Sentrux behavior for Runtime V2 work and
defines how later Runtime V2 tasks must use repository and scoped scans.

## Current Observed Behavior

Sentrux stores an active scan context. `health` and `check_rules` report on the
last path scanned, so every evidence section must name the exact path used
immediately before those calls.

Observed scan paths:

- Repository root: `/home/zov/projects/surge/surge`
- Runtime scope: `/home/zov/projects/surge/surge/runtime`

The repository root scan returned:

| Field | Value |
| --- | --- |
| Path | `/home/zov/projects/surge/surge` |
| Files | 4713 |
| Import edges | 1887 |
| Lines | 367877 |
| `quality_signal` | 6210 |
| Health bottleneck | `modularity` |
| Cross-module edges | 1820 |
| Root-cause scores | `acyclicity=10000`, `depth=6667`, `equality=4696`, `modularity=3435`, `redundancy=8588` |

For the repository root, `check_rules` reported:

```text
No rules file found at /home/zov/projects/surge/surge/.sentrux/rules.toml. Create one to define architectural constraints.
```

The runtime scoped scan returned:

| Field | Value |
| --- | --- |
| Path | `/home/zov/projects/surge/surge/runtime` |
| Files | 32 |
| Import edges | 30 |
| Lines | 14883 |
| `quality_signal` | 5147 |
| Health bottleneck | `redundancy` |
| Cross-module edges | 0 |
| Root-cause scores | `acyclicity=10000`, `depth=8889`, `equality=4735`, `modularity=3333`, `redundancy=2574` |

For the runtime scoped scan, `check_rules` reported:

```text
No rules file found at /home/zov/projects/surge/surge/runtime/.sentrux/rules.toml. Create one to define architectural constraints.
```

Filesystem inspection also found no `.sentrux/` directory at the repository root
and no `runtime/.sentrux/` directory in this checkout.

## Rule File Decision

Epic 1 Task 3 should create neither `.sentrux/rules.toml` nor
`runtime/.sentrux/rules.toml` yet.

The evidence currently proves only that Sentrux can scan both paths and that
`check_rules` has no configured rules for either active context. It does not
prove the correct rule schema or the exact architecture-level constraints to
encode. Creating a rules file now would risk turning draft Runtime V2 design
notes into machine-enforced internals before the owner, wakeup, cancellation,
and liveness policies are fully frozen.

Missing rules are therefore not a successful rule check. They are a recorded
open blocker for Runtime V2 rule enforcement. Docs-only Epic 1 tasks may
complete while recording this blocker. Runtime-code tasks after Epic 1 must not
claim Sentrux rule compliance until the relevant rules file exists or the epic
explicitly accepts a temporary deferral.

## Required Policy For Future Tasks

Before a Runtime V2 task starts, run and record:

1. `mcp__sentrux.scan` on `/home/zov/projects/surge/surge`
2. `mcp__sentrux.health`
3. `mcp__sentrux.check_rules`
4. `mcp__sentrux.scan` on the main affected scoped path, usually
   `/home/zov/projects/surge/surge/runtime`
5. `mcp__sentrux.health`
6. `mcp__sentrux.check_rules`

For runtime-code tasks, call `session_start` only after scanning the scoped path
that will be used for the final delta. Do not scan another path between
`session_start` and `session_end` unless the scoped path is scanned again before
`session_end`.

Before a Runtime V2 task completes, run the same scoped scan again and record
`health`, `check_rules`, and the quality delta. Then run the repository root
scan again and record `health` and `check_rules`. The final evidence must name
which path was active for each Sentrux result.

Completion is blocked when any of these is true:

- the scoped `quality_signal` drops below the recorded baseline, unless the task
  is a documented proving spike or the epic accepts a recovery task;
- `check_rules` reports a violation after a rules file exists for the active
  scan path;
- a task claims Sentrux rule compliance while `check_rules` only reported a
  missing rules file;
- the evidence does not identify the exact scanned path for `health` or
  `check_rules`;
- `session_end` compares a different active path than the path used for
  `session_start`.

## Open Decisions And Blockers

- Decide the first machine-enforced Sentrux rule file location. Root rules would
  govern whole-repository scans. Runtime-scoped rules would govern
  `/home/zov/projects/surge/surge/runtime` scans. If both scans are mandatory
  rule gates, both active contexts eventually need rules.
- Define the minimal architecture-level constraints that belong in Sentrux
  rules. They should describe stable boundaries, not implementation-level locks,
  queues, or shard internals.
- Decide whether root, runtime-scoped, or both rule files are required before
  the first Epic 2 runtime-code task completes. Missing rules do not block
  docs-only Epic 1 closeout, but they do block claiming Runtime V2 rule
  compliance until real rules exist or an explicit temporary deferral is
  recorded.
- Confirm the Sentrux rules schema before creating either rules file.
