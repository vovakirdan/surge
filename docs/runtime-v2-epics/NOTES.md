# Runtime V2 Working Notes

This file is the live handoff log for Runtime V2 work. Keep it current enough
that another session can resume without reconstructing context from chat.

Durable decisions must move into the relevant epic document, `README.md`,
`RULES.md`, `docs/RUNTIME_V2.md`, or another linked document before an epic
closes.

## Current State

- Runtime V2 target architecture lives in `docs/RUNTIME_V2.md`.
- Epic documents live in `docs/runtime-v2-epics/`.
- `README.md` contains the current epic roadmap and standing migration goals.
- `01-contract-rules-harness.md` is the active first epic draft.
- `RULES.md` contains global working rules.
- Task breakdown for Epic 1 exists in
  `docs/runtime-v2-epics/01-contract-rules-harness-tasks.md`.
- Epic 1 Task 2 is complete in this checkout. Scope stayed docs/evidence only:
  classify scheduler semantics into language contracts vs implementation
  artifacts, update Epic 1 and notes, update Task 2 status, and run
  `git diff --check`.
- Epic 1 Tasks 3, 4, and 5 are complete after parallel implementer subagents,
  separate reviewer subagents, and parent integration.
- Task 3 produced `SENTRUX_POLICY.md`.
- Task 4 produced `EVIDENCE_TEMPLATE.md`.
- Task 5 produced `01-baseline-evidence.md`.
- Shared integration updated `README.md`,
  `01-contract-rules-harness.md`, this notes file, and the task status table.

## Decisions Made

- Work proceeds epic-by-epic. Later epics stay as a short roadmap until earlier
  evidence shapes the next slice.
- Epic 1 Task 1, writing Runtime V2 development rules, is considered complete
  for now. Future rule changes may be added as discoveries, but they should not
  block starting Task 2.
- Epic 1 detailed task order is: scheduler semantic classification, sentrux
  rule policy, evidence template, baseline refresh, liveness probe plan, open
  decisions before Epic 2, closeout consolidation.
- `MUST` rules block completion, but proving spikes may temporarily violate a
  `MUST` if they define hypothesis, scope, proof, success/failure criteria, and
  rollback before implementation.
- Runtime must be explainable through ownership, wakeup, cancellation,
  lifetime/generation, backpressure, and trace/test visibility.
- Sentrux is mandatory. Use both repository and scoped scans when a task mostly
  affects `runtime/`.
- Runtime V2 code limit is 500 lines for new or heavily rewritten code files.
- New V2 C APIs use explicit status codes for recoverable failures. `panic_msg`
  is not the primitive error-handling contract.
- Working notes must be updated before tasks, after changes, after evidence
  runs, and when a path is proven wrong.
- Epic 1 Task 2 classification is now recorded in
  `01-contract-rules-harness.md`.
- Task 2 review found no substantive classification issues. It did find that
  two table cells mixed V2 target policy into the language-contract column; the
  wording was moved out of that column.
- Channel FIFO, task parking at suspension points, cooperative cancellation,
  structured join/failfast outcomes, and `@local spawn` sendability rules are
  source-visible contracts.
- Native global FIFO waiters, global inject, worker-local queues, Tier 1 work
  stealing, direct channel handoff placement, and sync-channel compensation are
  current implementation artifacts unless a later spec promotes one explicitly.
- VM/native parity means semantic output parity under native `threads=1`, not
  identical scheduler interleavings.
- Sentrux policy is now recorded in `SENTRUX_POLICY.md`. No root or
  runtime-scoped rules file exists yet. Missing rules are an open blocker for
  claiming rule compliance, not a passing rule check.
- Runtime V2 evidence format is now recorded in `EVIDENCE_TEMPLATE.md`.
  Proving spikes must record allowed files/surfaces, non-final behavior, proof
  test or benchmark or trace or invariant, success criteria, failure criteria,
  and rollback before implementation.
- Baseline evidence is now recorded in `01-baseline-evidence.md`.
- The focused VM baseline command fails without timeout-test skipping:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`.
- Default `make check` passes because Makefile default
  `SURGE_SKIP_TIMEOUT_TESTS=1` skips timeout-sensitive VM/LLVM tests through
  `skipTimeoutTests`.

## Current Sentrux Baselines

- Repository scan: `/home/zov/projects/surge/surge`, `quality_signal=6210`.
- Runtime scoped scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5147`, bottleneck `redundancy`.
- `check_rules` currently reports no `.sentrux/rules.toml` for either scanned
  path. `SENTRUX_POLICY.md` records the decision not to create draft rules in
  Task 3; runtime-code tasks after Epic 1 must either add real rules or record
  an explicit temporary deferral.

## Known Large Files

These files already exceed the 500-line Runtime V2 limit and need care when
touched:

- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

Touching an over-limit file must record whether the task reduces it, keeps it
flat, or creates a follow-up split task.

## Tested In This Planning Pass

- `mcp__sentrux.scan` works for the whole repo and for `runtime/` as a scoped
  directory.
- `git diff --check` passed after current markdown-only edits.
- `git diff --check` passed again after the Task 2 table, notes update, and Task
  2 status update.
- Because `docs/runtime-v2-epics/` is still untracked, `git diff --check` does
  not cover those files yet. The untracked markdown files were also checked with
  `git diff --no-index --check /dev/null <file>`; those commands returned
  no whitespace output. The non-zero `--no-index` exit code is expected for
  different files, so use empty output as the whitespace signal until the files
  are tracked.
- Task 2 evidence search used:

  ```bash
  rg -n "FIFO|order|fair|determin|wake|cancel|join|spawn|timeout|shutdown|channel|select" docs internal/vm internal/asyncrt
  ```

- Relevant evidence slices were read from `docs/CONCURRENCY.md`,
  `docs/LANGUAGE.md`, `docs/RUNTIME.md`, `docs/RUNTIME_V2.md`,
  `internal/asyncrt`, `internal/vm/mt_executor_test.go`,
  `internal/vm/mt_correctness_test.go`, and VM async shutdown/borrow tests.
- Task 3 Sentrux review accepted `SENTRUX_POLICY.md` and independently
  confirmed the root/runtime scan values.
- Task 4 review required stronger proving-spike fields and a docs-only example
  that includes both `git diff --check` and the untracked-file `--no-index`
  whitespace check. The template was updated.
- Task 5 review required proof for the focused VM failure vs `make check`
  pass. Parent verification ran:

  ```bash
  SURGE_SKIP_TIMEOUT_TESTS=1 go test ./internal/vm -run 'MT|Async|Net|LLVM' --timeout 90s
  SURGE_SKIP_TIMEOUT_TESTS=1 go test ./internal/vm -run 'LLVMParity|MTCorrectnessHTTPServer|VMTerm' -v --timeout 90s
  ```

  The first command passed with `ok surge/internal/vm 1.260s`. The second
  command showed the matching LLVM parity, MT HTTP, and VM terminal tests were
  skipped by `skipTimeoutTests`.
- Task 5 baseline commands recorded in `01-baseline-evidence.md`:
  `git diff --check`, `make c-check`, `make cppcheck`, focused VM test,
  `make check`, native net benchmark, and native channel benchmark.
- Parent integration repeated Sentrux checks after Tasks 3-5 doc updates:
  root stayed `quality_signal=6210` with bottleneck `modularity`; runtime stayed
  `quality_signal=5147` with bottleneck `redundancy`; both `check_rules` calls
  still report missing rules files.

## Not Tested Yet

- Runtime/native behavior for the new docs. No runtime code changed.
- Full current-checkout focused VM repair. The failure is recorded as baseline
  debt, not fixed in Epic 1.
- Sentrux rule compliance. Rule files do not exist yet.
- Sentrux `session_end` after code changes. No code changes happened yet.

## Dead Ends And Cautions

- Do not tune scheduler behavior by machine-specific constants as a durable
  design.
- Do not let proving-spike code become architecture without rewriting it into
  rule-compliant form.
- Do not leave important decisions only in this notes file.
- The first broad `rg` result was too large and included duplicate translated
  docs. The table uses narrowed English docs/tests and current Go runtime
  slices instead.
- Do not use `TestMTWorkStealing` as a future Tier 1 contract without deciding
  whether the assertion moves to explicit Tier 2 CPU work.

## Open Decisions

- Whether crossing propagates into function signatures through a `crosses`
  marker. This can wait until the explicit crossing language-surface epic unless
  it blocks the contract table.
- Whether to create `.sentrux/rules.toml` for `runtime/`, repository root, or
  both. The Task 3 policy defers creation until constraints are known.
- Timer clock wording conflicts between `docs/CONCURRENCY.md` and
  `docs/LANGUAGE.md`; resolve before timer implementation work.
- Native shutdown parity/liveness evidence is still thin. Add or identify a
  native probe before moving shutdown state.
- Before Epic 2 starts, decide whether the current focused VM failure is fixed
  first or accepted as pre-existing debt for a narrowly scoped structural task.
