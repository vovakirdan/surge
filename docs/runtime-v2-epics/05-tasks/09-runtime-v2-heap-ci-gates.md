# Task 9: Runtime V2 Heap CI Gates

**Status:** Draft
**Kind:** CI

## Goal

Add stable heap-accounting tests to local Runtime V2 gates and CI.

## Scope

- Add `runtime-v2-heap-check` to `Makefile`.
- Wire `runtime-v2-heap-check` into `make runtime-v2-check`.
- Ensure GitHub Actions already running `make runtime-v2-check` covers the new
  heap gate.
- Include only stable tests; local-only stress or benchmark probes stay out of
  CI with explicit evidence.
- Update task evidence with exact commands and expected CI coverage.

## Files

- Modify: `Makefile`
- Modify if needed: `.github/workflows/ci.yml`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify if needed: `docs/runtime-v2-epics/LIVENESS_PROBES.md`

## Checks

- `make runtime-v2-heap-check`
- `make runtime-v2-check`
- `make check`
- `git diff --check`

## Done

- Stable heap-accounting tests run under `make runtime-v2-check`.
- CI inherits the heap gate through the existing Runtime V2 workflow.
- Local-only heap probes are named and excluded deliberately.
