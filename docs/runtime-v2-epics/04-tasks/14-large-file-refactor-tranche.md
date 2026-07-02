# Task 14: Large-File Refactor Tranche

**Status:** Complete
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
- `runtime/native/rt_net_trace.h`
- `runtime/native/rt_net_trace.c`
- `.loc-legacy-allowlist`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/04-tasks/README.md`

## Implementation

- Boundary chosen: `TRACE_NET` trace counters and dump helpers were moved out
  of `rt_net.c` into `rt_net_trace.c`.
- `rt_net.c` keeps wake-fd init/write/drain, `poll()` construction, registry
  snapshots, and close lifecycle.
- `rt_net_trace_dump(const char*)` remains externally visible and preserves
  the existing `TRACE_NET` field names and order.
- Trace counters remain private `static` atomics inside `rt_net_trace.c`.
- `rt_net_trace.h` exposes prototypes for every non-static helper and uses
  inline guard wrappers so disabled tracing avoids atomic updates.
- `.loc-legacy-allowlist` lowered `runtime/native/rt_net.c` from `1002` to
  the exact post-refactor count, `904`.

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

Focused implementation checks run in this slice:

- `clang-format -i runtime/native/rt_net.c runtime/native/rt_net_trace.c runtime/native/rt_net_trace.h`
- `wc -l runtime/native/rt_net.c runtime/native/rt_net_trace.c runtime/native/rt_net_trace.h runtime/native/rt_async_internal.h`
- `make c-check`
- `make cppcheck`
- `make runtime-v2-fd-registry-check`
- `git diff --check`
- `git diff --no-index --check /dev/null runtime/native/rt_net_trace.{c,h}`
- `./check_file_sizes.sh`

Final main-session checks:

- `make runtime-v2-check`
- `make check`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v --timeout 90s`
- native net benchmark with current working-tree binary
- Sentrux root and scoped scans

## Done

- Behavior is unchanged by the refactor.
- Touched over-limit files shrink or have a documented reason if they do not.
- Any deleted code has complete evidence.
