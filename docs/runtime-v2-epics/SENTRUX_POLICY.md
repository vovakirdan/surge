# Runtime V2 Sentrux Policy

This policy records the current Sentrux behavior for Runtime V2 work and
defines how later Runtime V2 tasks must use repository and scoped scans.

## Pre-Epic 4 Update

Sentrux rule files now exist for every mandatory Runtime V2 scan root:

- `.sentrux/rules.toml`
- `runtime/.sentrux/rules.toml`
- `runtime/native/.sentrux/rules.toml`

The files follow the public Sentrux examples: a `[constraints]` table plus
optional `[[layers]]` and `[[boundaries]]` entries. The initial thresholds are
baseline-preserving, not ideal-state targets. Tightening them is tracked in
`DEBT.md`.

Current local validation:

- `sentrux check .`: passed, quality `6198`.
- `sentrux check runtime`: passed, quality `5195`.
- `sentrux check runtime/native`: passed, quality `5159`.
- MCP `check_rules`: passed for root, `runtime/`, and `runtime/native`.

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

Pre-Epic 4 quality hardening closed the missing-rules blocker. Missing Sentrux
rules are no longer an accepted Runtime V2 state for the repository root,
`runtime/`, or `runtime/native/`.

The initial rules are deliberately conservative:

- quality floors are just below the current baseline;
- complexity and function-length ceilings match current legacy reality rather
  than the ideal Runtime V2 target;
- root layers encode broad direction only;
- root boundaries prevent native runtime code from depending on Go compiler,
  VM, or CLI internals.

Future tasks should tighten these rules only with evidence. Do not encode
implementation details such as lock ordering or transient queue internals in
Sentrux rules.

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

- Tighten `max_cc`, `max_fn_lines`, and quality floors after large-file
  refactors remove the current legacy ceiling pressure.
- Add more architecture-level boundaries only after the affected ownership
  contract is stable in `docs/RUNTIME_V2.md`.
