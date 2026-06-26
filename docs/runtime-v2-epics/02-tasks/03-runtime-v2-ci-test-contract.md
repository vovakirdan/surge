# Epic 2 Task 03: Runtime V2 Test And CI Contract

**Goal:** define which Epic 2 tests are stable enough for CI and which remain
local evidence only.

**Approach:** choose named probes from `LIVENESS_PROBES.md`, not broad regexes
with accepted debt. Create a CI contract before adding tests so new runtime work
cannot hide behind skipped timeout-sensitive tests.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `.github/workflows/ci.yml`, `Makefile`,
`internal/vm/*_test.go`, `docs/runtime-v2-epics/LIVENESS_PROBES.md`

---

## Files

- Modify: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Create or modify: `docs/runtime-v2-epics/02-ci-test-contract.md`

## Steps

1. List all existing probes that can run with `SURGE_SKIP_TIMEOUT_TESTS=0`.
2. Exclude the broad accepted-debt command
   `go test ./internal/vm -run 'MT|Async|Net|LLVM'` from required CI gates.
3. Pick a small stable Runtime V2 CI subset by exact test name.
4. Define a future `make runtime-v2-check` or equivalent CI command.
5. Define how new tests join that target:
   - local pass with timeout wrapper;
   - evidence recorded in the task;
   - no known nondeterministic hang;
   - clear failure message and captured output.
6. Record which probes stay local-only until the test/backend matrix epic.
7. Run `git diff --check`.

## Done

- Epic 2 has a written CI contract.
- CI scope separates accepted debt from new Runtime V2 gates.
- Later test-writing tasks know where their tests must be wired.
