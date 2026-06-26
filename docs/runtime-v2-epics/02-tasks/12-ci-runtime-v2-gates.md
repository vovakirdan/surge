# Epic 2 Task 12: CI Runtime V2 Gates

**Goal:** add stable Runtime V2 tests to CI so liveness regressions cannot stay
hidden behind skipped timeout-sensitive tests.

**Approach:** add a dedicated target and workflow job for named stable probes.
Do not require the accepted-debt broad focused VM command.

**Skills:** `ci-cd-best-practices`, `writing-clearly-and-concisely`

**Tech Details:** `Makefile`, `.github/workflows/ci.yml`,
`SURGE_SKIP_TIMEOUT_TESTS=0`, GitHub Actions

---

## Files

- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `docs/runtime-v2-epics/02-ci-test-contract.md`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Read Task 03 CI contract and test evidence from Tasks 04, 06, 08, and 10.
2. Add a make target such as `runtime-v2-check` with exact stable test names.
3. Set `SURGE_SKIP_TIMEOUT_TESTS=0` for that target.
4. Add a CI job that runs the target on pull requests and pushes.
5. Keep timeout minutes bounded and upload logs/artifacts if a probe produces
   files.
6. Do not add the broad accepted-debt focused VM command as a required pass.
7. Run the new target locally.
8. Run `make check`.

## Done

- CI includes at least one stable Runtime V2 liveness gate.
- The gate runs timeout-sensitive tests deliberately, not through default skip.
- Any excluded probe has a named blocker and owner.
