# Epic 14 Task 5b: Diagnostics Precision Across The Crossing Family

**Status:** complete (2026-07-12).
**Kind:** guard-stage diagnostics (classifier + message content), test re-pins.
**Depends on:** Task 4 (capability flip), Task 5 (negative matrix).

## What Landed

Every guarded crossing record is now classified by its real cause instead
of collapsing into the per-form "backend unavailable" message:

- **FUT7019 (sync context).** A crossing whose only blocker is the missing
  async context says so — "suspends until its reply arrives … make the
  enclosing function `async`" — uniformly on every backend, because the
  fix is the same everywhere. This is the kindness-first template from the
  epic's decision 8.
- **FUT7020 (payload/capture).** A crossing whose shape cannot ship names
  the culprit at the culprit's span: the capture binding and type
  ("capture `m` (`own Movable`) moves owned data …"), the reply payload
  with the exact nested field path (`NonCopyCulpritPath`: "the crossing
  result `Report` … (field `meta.name` owns heap memory)"), the anchored
  union reply with the in-body unwrap fix, the channel element for
  `channel_on`, and the captured `far Task` lease. Backend-independent for
  the same reason.
- **Generic FUT7014-7018 survive only on genuinely backend-blocked rows**:
  executable async shapes compiled for a backend without the transport.

Mechanics: `classifyCrossingGuard` (buildpipeline) walks the sema crossing
records with the per-module strings interner (dependency modules
included); sema records the capture's source name at check time so no
symbol-table round trip is needed; `NonCopyCulpritPath` walks struct
fields to the first non-copy leaf.

## Re-pins

- The five sync golden fixtures became the FUT7019 pins (uniform on VM
  and LLVM); the generic-message pins moved to async inline fixtures
  compiled on transportless backends only.
- `TestCrossingBackendGuardsAreDefaultClosed` drops its LLVM rows — the
  forms are open there by design and pinned open by the capability tests
  and e2e suites.
- Imported-module tests now count cause findings (sync-context ×6,
  payload ×2), which still proves the dependency-module walk.
- `TestCrossingPayloadDiagnosticNamesTheField` pins the message content:
  the field path, the unwrap fix, and the capture name.

## Closeout

Gates: `make check`, golden-check (fixture-only churn, diag re-pins),
transport + crossing gates green. Sentrux (committed tree): root `6183`
(RV2-DEBT-029 unchanged, equality `0.4487`), `internal` `6508`, `runtime`
`5310`, `runtime/native` `5399`.

Deliberately out of scope: moving the sync-context/payload classification
into sema proper (the guard stage owns backend knowledge; the messages
are already cause-precise) and relaxing the payload representation —
both ride later work (union wire format; RV2-DEBT-030 family).
