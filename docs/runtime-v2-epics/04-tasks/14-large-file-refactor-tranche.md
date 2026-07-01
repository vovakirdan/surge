# Task 14: Large-File Refactor Tranche

**Status:** Draft
**Kind:** refactor code

## Goal

Reduce large-file pressure created or exposed by fd-registry work without
changing behavior.

## Scope

- Use the Task 2 dependency map and completed registry behavior tests.
- Split cohesive registry, poll construction, or lifecycle helpers out of
  `rt_net.c` only when the boundary is clear.
- Keep new files under the 500-line Runtime V2 target.
- Delete dead code only with reference, build, test, and Sentrux evidence.
- Avoid unrelated filesystem, terminal, channel, or compiler refactors.

## Files

- `runtime/native/rt_net.c`
- fd registry implementation/header files
- any new cohesive net-registry module justified by the map
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- before/after line counts
- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- all stable fd-registry tests
- native net benchmark if code movement touches hot poll paths
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Behavior is unchanged by the refactor.
- Touched over-limit files shrink or have a documented reason if they do not.
- Any deleted code has complete evidence.
