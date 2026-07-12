# Epic 15 Task 2: Gate Manifest + Integrity Meta-Test

**Status:** complete (2026-07-12).
**Kind:** tooling; runs inside `make check` by construction.

## What Landed

`internal/gatecheck`: the Makefile IS the manifest source of truth — the
checker parses every `go test` recipe line (target, packages, tags, -run)
and both Makefile edge kinds (prerequisites and `$(MAKE)` calls) from the
`check` and `runtime-v2-check` roots. Selection is evaluated by invoking
`go test -list` under each gate's own tags — never a regex
reimplementation. Assertions:

- every gate's selection matches at least one test;
- every gate target is reachable from a root or carries an owned
  exemption (`exemptions.txt`: kind/name/owner/reason; unowned entries
  rejected);
- every test that exists ONLY under a runtime build tag is selected by
  some gate (default-tag tests ride `go test ./...`; the tagged inventory
  is the set difference, since a build tag adds files);
- bootstrap canary: the test target keeps `go test ./...` and the check
  root keeps reaching it — the meta-test's own reachability is asserted,
  its enforcement is the `./...` run itself;
- both rot modes have isolated negative controls on synthetic inputs.

Exemption ledger seeded with: `runtime-v2-liveness-stress` (quarantined,
owner recorded — the 1b bound), `bench`, and the `golden` tag (runs via
`scripts/golden.sh`).

## What The First Run Caught (immediately)

- **14 orphaned tagged tests**, wired into their natural gates: the
  remote-channel self-deadlock rows (added this week, never gated — the
  exact rot class this task exists for), six fd-registry cancelled-waiter
  /close-wake rows, the lifecycle cancel-race TSan variant, the placement
  ABI/resolver rows, three scheduler-placement rows, and the executor
  skeleton static.
- **One real pre-existing break**: the fd-registry shutdown harness (a
  deliberate stub-isolation build) missed the `rt_far_channel_release_all`
  stub added to the shutdown path by the channel registry — a link
  failure invisible since Epic 14 Task 1.5 because the gate rarely ran.
  Fixed with the stub; the harness stays stub-isolated by design.
- One transport-gate transient on first run (RV2-DEBT-018 class), green
  on rerun.

## Gates

`internal/gatecheck` green (meta-test + negative controls); the four
touched runtime gates green after wiring; `make check` green.
