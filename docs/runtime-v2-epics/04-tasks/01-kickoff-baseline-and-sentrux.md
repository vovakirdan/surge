# Task 1: Kickoff Baseline And Sentrux

**Status:** Draft
**Kind:** evidence

## Goal

Record the exact Epic 4 starting state before fd-registry work begins.

## Scope

- Capture branch, commit, and `git status --short`.
- Capture line counts for touched runtime/native files.
- Record accepted backend-test debt and current Sentrux rule status.
- Run root, `runtime/`, and `runtime/native/` Sentrux scans.
- Define the concrete gates for Tasks 2-7.

## Files

- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/04-persistent-fd-registry-and-net-lifecycle.md`

## Checks

- `git diff --check`
- Sentrux repository scan
- Sentrux `runtime/` scan
- Sentrux `runtime/native/` scan

## Done

- Starting evidence is recorded.
- Sentrux rule status is recorded as pass/fail evidence.
- Next task has enough context to map fd dependencies without rediscovery.
