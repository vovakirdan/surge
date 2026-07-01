# Task 4: Registry Static Shape Tests

**Status:** Draft
**Kind:** test/static checks

## Goal

Add static checks that protect the owner boundary for the fd registry skeleton.

## Scope

- Compile-check the intended registry declarations or placeholder contract.
- Assert the registry is owned by the shard/runtime owner path, not by a global
  executor-wide rebuild helper.
- Assert new V2 registry APIs use explicit status codes for recoverable
  failures.
- Keep the test independent from runtime behavior where possible.

## Files

- `internal/vm/runtime_v2_fd_registry_static_test.go`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `go test ./internal/vm -run TestRuntimeV2FDRegistryStatic -v --timeout 90s`
- `git diff --check`

## Done

- Static guard fails before the skeleton exists or proves the current approved
  placeholder.
- The guard is narrow enough to survive refactors that keep the same contract.
