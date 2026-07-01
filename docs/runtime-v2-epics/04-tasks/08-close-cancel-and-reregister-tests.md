# Task 8: Close, Cancel, And Re-register Tests

**Status:** Draft
**Kind:** test writing

## Goal

Add focused tests for the riskiest fd lifecycle transitions before cleanup code
is migrated.

## Scope

- Cover cancelling one readiness interest while another remains active.
- Cover closing a listener or connection with parked net waiters.
- Cover stale readiness after close.
- Cover numeric fd reuse or an equivalent generation-guard proof.
- Keep probes deterministic enough for CI.

## Files

- `internal/vm/runtime_v2_fd_registry_contract_test.go`
- optional native C static/behavior helper if the Go surface cannot prove fd
  reuse deterministically
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- focused new tests in approved tag mode
- existing focused net wake probe from `LIVENESS_PROBES.md`
- `git diff --check`

## Done

- Tests fail only for the missing close/cancel/re-register implementation, or
  pass if Task 7 already covers the behavior.
- Evidence states the exact behavior that Task 9 must make green.
