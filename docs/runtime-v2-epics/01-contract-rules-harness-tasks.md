# Runtime V2 Contract, Rules, And Harness Task Breakdown

**Goal:** finish Epic 1 by turning Runtime V2 rules into contract decisions,
baseline evidence, liveness probes, and handoff-ready documentation.

**Approach:** keep this epic read-only with respect to runtime behavior. Each
task updates documents, evidence, or checks only. Do not start `rt_runtime`,
`rt_shard`, waiter, fd-registry, parser, sema, MIR, LLVM, or ABI work in this
epic.

**Skills:** @task-breakdown, @writing-clearly-and-concisely, @c-pro for C API
policy, sentrux MCP for quality gates.

**Tech Details:** `docs/runtime-v2-epics/`, `docs/RUNTIME_V2.md`,
`docs/RUNTIME.md`, `docs/CONCURRENCY.md`, `runtime/native/`,
`internal/vm/`, `scripts/bench_native_net.sh`,
`scripts/bench_native_channels.sh`, sentrux `scan`, `health`, `check_rules`,
`session_start`, `session_end`.

---

## Status

| Task | Status | Output |
| --- | --- | --- |
| 1. Runtime V2 development rules | Done | `RULES.md`, `NOTES.md`, README/Epic links |
| 2. Scheduler semantic classification | Done | Contract/artifact table |
| 3. Sentrux rule policy | Done | `SENTRUX_POLICY.md`; missing rules recorded as blocker |
| 4. Evidence template | Done | `EVIDENCE_TEMPLATE.md` |
| 5. Baseline evidence refresh | Done | `01-baseline-evidence.md`; focused VM blocker recorded |
| 6. Liveness probe plan | Done | `LIVENESS_PROBES.md` |
| 7. Open decisions before Epic 2 | Done | `OPEN_DECISIONS_BEFORE_EPIC_2.md` |
| 8. Epic closeout consolidation | Pending | Durable docs updated from notes |

## Standing Step For Every Task

Before starting a task, update `docs/runtime-v2-epics/NOTES.md` with current
context and intended proof. After the task, update it with what changed, what
was checked, what was not checked, and any dead end.

Run after every docs-only task:

```bash
git diff --check
```

Expected: no output and exit code 0.

## Task 1: Runtime V2 Development Rules

**Status:** Done.

**Files:**

- Created: `docs/runtime-v2-epics/RULES.md`
- Created: `docs/runtime-v2-epics/NOTES.md`
- Modified: `docs/runtime-v2-epics/README.md`
- Modified: `docs/runtime-v2-epics/01-contract-rules-harness.md`

**Done means:**

- proving-spike rule exists;
- runtime explainability rule exists;
- sentrux gate exists;
- 500-line code limit exists;
- reuse-before-new-machinery rule exists;
- required checks rule exists;
- comments/names rule exists;
- owner-oriented C and explicit status-code rule exists;
- notes rule exists.

**Verification already run:**

```bash
git diff --check
```

Expected: pass.

## Task 2: Scheduler Semantic Classification

**Status:** Done.

**Goal:** separate Surge async language contracts from current runtime
implementation artifacts before any `N=1` structural work.

**Files:**

- Read: `docs/RUNTIME.md`
- Read: `docs/CONCURRENCY.md`
- Read: `docs/RUNTIME_V2.md`
- Read: `internal/vm/mt_executor_test.go`
- Read: `internal/vm/mt_correctness_test.go`
- Read: `internal/vm/vm_async_*_test.go`
- Read: `internal/asyncrt/*.go`
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

**Steps:**

1. Update `NOTES.md` with Task 2 start context.
2. Search current docs and tests for observable scheduler claims:
   `rg -n "FIFO|order|fair|determin|wake|cancel|join|spawn|timeout|shutdown|channel|select" docs internal/vm internal/asyncrt`.
3. Read the smallest relevant slices from the matched files.
4. Add a table to Epic 1 with columns: behavior, language contract,
   implementation artifact, evidence, decision before Epic 2.
5. Classify at least: FIFO waiter behavior, worker placement, work stealing,
   local spawn, join/cancel, channel handoff, timers, shutdown, VM/native parity.
6. Update `NOTES.md` with decisions and uncertain items.
7. Run `git diff --check`.

**Expected output:** Epic 1 contains a contract/artifact table good enough to
guide the first `rt_executor` field moves.

## Task 3: Sentrux Rule Policy

**Status:** Done.

**Goal:** decide how sentrux checks apply to Runtime V2 and remove ambiguity
around missing `.sentrux/rules.toml`.

**Files:**

- Inspect: `.sentrux/rules.toml` if present
- Inspect: `runtime/.sentrux/rules.toml` if present
- Created: `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- Modify or create: `.sentrux/rules.toml` only if we decide root rules belong in
  this epic
- Modify or create: `runtime/.sentrux/rules.toml` only if scoped runtime rules
  are appropriate
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

**Steps:**

1. Update `NOTES.md` with Task 3 start context.
2. Run repository sentrux scan:
   `mcp__sentrux.scan(path="/home/zov/projects/surge/surge")`.
3. Run runtime sentrux scan:
   `mcp__sentrux.scan(path="/home/zov/projects/surge/surge/runtime")`.
4. Run `mcp__sentrux.health` and `mcp__sentrux.check_rules` after the scoped
   scan.
5. Decide whether this epic creates sentrux rules or records a blocker.
6. If rules are created, keep them minimal and architecture-level. Do not encode
   implementation-specific locks or queue internals.
7. Update Epic 1 and `NOTES.md` with the policy.
8. Run `git diff --check`.

**Expected output:** later tasks know exactly which sentrux path to scan and
whether missing sentrux rules are a blocker.

## Task 4: Evidence Template

**Status:** Done.

**Goal:** create a reusable evidence format that every Runtime V2 task and epic
can fill in.

**Files:**

- Create or modify: `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- Modify: `docs/runtime-v2-epics/README.md`
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

**Steps:**

1. Update `NOTES.md` with Task 4 start context.
2. Draft the template sections: task, baseline commit, scope, files touched,
   contracts touched, sentrux root/scoped signals, commands, benchmarks, trace
   counters, liveness proof, known regressions, dead ends, rollback/recovery,
   follow-ups.
3. Add a short example row for a docs-only task and a runtime-code task.
4. Link the template from `README.md` and Epic 1.
5. Update `NOTES.md` with the template location.
6. Run `git diff --check`.

**Expected output:** every future task can copy a single template instead of
inventing its own proof format.

## Task 5: Baseline Evidence Refresh

**Status:** Done.

**Goal:** capture current checkout evidence before Epic 2 changes runtime
structure.

**Files:**

- Create: `docs/runtime-v2-epics/01-baseline-evidence.md`
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

**Steps:**

1. Update `NOTES.md` with Task 5 start context.
2. Record `git rev-parse HEAD` and `git status --short`.
3. Run sentrux root scan and scoped runtime scan. Record path,
   `quality_signal`, bottleneck, and rule status.
4. Run:

   ```bash
   git diff --check
   make c-check
   make cppcheck
   go test ./internal/vm -run 'MT|Async|Net|LLVM'
   make check
   ```

5. Run native benchmarks if the local machine is ready:

   ```bash
   ./scripts/bench_native_net.sh
   ./scripts/bench_native_channels.sh
   ```

6. If any command is skipped or fails, record the exact reason and whether it
   blocks Epic 2.
7. Record report paths and key counters from generated benchmark reports.
8. Update Epic 1 and `NOTES.md`.

**Expected output:** a current baseline document that Epic 2 can compare against.

## Task 6: Liveness Probe Plan

**Status:** Done.

**Goal:** define which liveness checks are mandatory before runtime scheduler,
wakeup, waiter, channel, timer, cancellation, or shutdown changes are accepted.

**Files:**

- Created: `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- Read: `internal/vm/mt_executor_test.go`
- Read: `internal/vm/mt_correctness_test.go`
- Read: `runtime/native/rt_async_state.c`
- Read: `runtime/native/rt_net.c`
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

**Steps:**

1. Update `NOTES.md` with Task 6 start context.
2. Inventory existing liveness-oriented tests and trace hooks.
3. Define required probes for net wakeups, channel wakeups, cancellation races,
   timeout/shutdown, parked-with-work invariants, SIGUSR1 live trace snapshots,
   and benchmark timeout wrappers.
4. Mark which probes already exist and which must be written before Epic 2 or a
   later runtime-code epic.
5. Link the probe plan from Epic 1 and `README.md`.
6. Update `NOTES.md`.
7. Run `git diff --check`.

**Expected output:** future runtime changes have a concrete liveness checklist,
not a vague "watch for hangs" instruction.

## Task 7: Open Decisions Before Epic 2

**Status:** Done.

**Goal:** make sure Epic 2 can start without unresolved global-policy questions.

**Files:**

- Created: `docs/runtime-v2-epics/OPEN_DECISIONS_BEFORE_EPIC_2.md`
- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify: `docs/runtime-v2-epics/README.md` if roadmap wording changes

**Steps:**

1. Update `NOTES.md` with Task 7 start context.
2. Review all open decisions in Epic 1 and `NOTES.md`.
3. Split them into: blocks Epic 2, can wait until explicit crossing, can wait
   until multi-shard runtime, can wait until allocator/pools.
4. Decide the status of `crosses`, sentrux rules, VM/native parity guarantees,
   and what behavior must remain equivalent in `N=1`.
5. Update Epic 1 exit criteria if needed.
6. Update `NOTES.md`.
7. Run `git diff --check`.

**Expected output:** Epic 2 has a clear start condition and no hidden policy
blockers.

## Task 8: Epic Closeout Consolidation

**Goal:** close Epic 1 by moving durable information out of working notes and
into the documents future work will read first.

**Files:**

- Modify: `docs/runtime-v2-epics/01-contract-rules-harness.md`
- Modify: `docs/runtime-v2-epics/README.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify: `docs/RUNTIME_V2.md` only if the target architecture itself changed

**Steps:**

1. Update `NOTES.md` with Task 8 start context.
2. Move durable decisions from `NOTES.md` into Epic 1, `README.md`, `RULES.md`,
   or `docs/RUNTIME_V2.md`.
3. Leave `NOTES.md` as a concise handoff summary for Epic 2.
4. Mark Epic 1 status as complete when acceptance gates are satisfied.
5. Run final docs check:

   ```bash
   git diff --check
   ```

6. If code or sentrux rules changed during the epic, run the applicable gates
   recorded in `RULES.md`.

**Expected output:** Epic 1 is complete, and Epic 2 can start from docs rather
than chat history.
