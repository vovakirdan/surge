# Task 3: FD Lifecycle Contract Tests

**Status:** Draft
**Kind:** test writing

## Goal

Add focused behavior tests that describe the desired fd registry lifecycle
before replacing the poll rebuild path.

## Scope

- Cover repeated readiness on one fd.
- Cover read and write interest on the same fd without duplicate fd rows.
- Cover duplicate waiter interest where current semantics allow it.
- Cover closed fd behavior at the Surge program or native test level.
- Keep tests focused enough to become stable Runtime V2 gates later.

## Files

- `internal/vm/runtime_v2_fd_registry_contract_test.go`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- Default tag-off Go test for the new file, if pending tags are used
- Tagged pending proof, if the tests intentionally describe future behavior
- Existing focused net liveness probe selected from `LIVENESS_PROBES.md`
- `git diff --check`

## Done

- Tests state the intended fd lifecycle behavior without changing runtime C.
- Any intentionally failing future proof is isolated behind a pending tag.
- Evidence says which failures are expected before implementation.
