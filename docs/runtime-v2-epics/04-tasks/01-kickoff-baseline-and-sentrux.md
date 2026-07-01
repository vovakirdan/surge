# Task 1: Kickoff Baseline And Sentrux

**Status:** Draft
**Kind:** evidence

## Goal

Record the exact Epic 4 starting state before fd-registry work begins.

## Scope

- Capture branch, commit, and `git status --short`.
- Capture line counts for touched runtime/native files.
- Record accepted backend-test debt and Sentrux missing-rules debt.
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
- Missing-rule status is named as debt, not compliance.
- Next task has enough context to map fd dependencies without rediscovery.
