# D2 relative stdlib-root identity certificate

Verified 2026-10-09. Base: `c4316ab9ac132c8f5da7d184755be2a0d67f9df0`.
Source candidate: `438be3a9deb311d96a6940e7ab43180ee32294f4`.

## What changed

Return-origin admits a `stdlib/...` module identity only when the physical file
is inside the configured stdlib root. For a CLI root supplied as the relative
path `stdlib/time/time.sg`, FileSet retained that relative spelling while
`SURGE_STDLIB` was absolute. `pathWithin` therefore rejected the real file and
cleared its `ModulePath`; the existing exact Duration certificate then refused
`monotonic_now`, `Duration.now` and `Duration.sub`.

The publication collector now resolves a relative source path against the
FileSet base directory before the physical containment check. Absolute paths
take the unchanged path. A matching module spelling outside the configured
stdlib root still loses stdlib identity and remains fail-closed.

## Verification

| Check | Result and limit |
| --- | --- |
| Focused contracts | Relative standalone `stdlib/time/time.sg`, imported Duration, directive/golden rows, forged-module canaries and core-root absolute/relative guards PASS. |
| Compiler package | Focused `internal/driver` PASS; `go vet ./internal/driver` PASS. |
| Counterfactual | Restoring only the previous raw-path comparison restores the exact six-row standalone `time.sg` result. |
| Targeted corpus | `stdlib/time/time.sg` moves from six unfinished rows to rc0. |
| Census | Exact clean-candidate sweep: 1187 programs, 39 unfinished, 0 timeouts. Exactly `stdlib/time/time.sg` loses its six rows and moves to rc0; no row is gained and no other status, code or reason set changes. |
| Structural | Sentrux keeps quality 0.53, coupling 0.08, one cycle, broad complex-function count 1083 and the old max-cc violation. The changed driver file is 272 lines and passes the size gate. |

## Limits

This changes path normalization for the physical identity check only. It does
not reserve the `stdlib` namespace, trust a module name outside the configured
root, alter Duration layout or widen opaque-result certificates. The existing
forged `stdlib/time` project outside the real root remains refused.

No runtime, MIR or backend behavior changes. Imported Duration programs were
already admitted; this packet makes the same declaration identity visible when
the real library file itself is the relative CLI root.

## Evidence

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-stdlib-relative-root/`:
candidate/counterfactual compilers, exact time records, focused tests, Sentrux
output and the resumable exact census.
