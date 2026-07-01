# Task 13: Runtime V2 FD Registry CI Gates

**Status:** Draft
**Kind:** CI

## Goal

Add stable fd-registry liveness tests to the Runtime V2 CI gate.

## Scope

- Promote only stable, non-flaky fd-registry tests.
- Keep pending or timeout-sensitive proofs out of required CI until stabilized.
- Update `make runtime-v2-check` if needed.
- Update CI workflow commands and documentation.

## Files

- `Makefile`
- `.github/workflows/*`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`, if the stable probe list changes

## Checks

- `make runtime-v2-check`
- `make check`
- exact promoted Go test command
- `git diff --check`

## Done

- CI covers stable fd-registry liveness.
- Pending tests remain clearly marked and excluded from default gates.
- Documentation names the exact stable command.
