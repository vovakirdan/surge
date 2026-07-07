# Epic 10 Task 5: Closeout

**Status:** complete.
**Kind:** docs/debt/evidence reconciliation.

## Closed Scope

Epic 10 closed all owned debts:

- `RV2-DEBT-003`: `rt_async_state.c` dependency split.
- `RV2-DEBT-010`: stable runtime net handle ids and stale copied-handle guard.
- `RV2-DEBT-013`: owner-local stdlib HTTP server path.

No language syntax, public crossing APIs, Phase 4 transport, inbound queues,
remote select, remote-free routing, or alternate I/O backend work landed.

## Code Commit

`9d1b06c1 fix(runtime): stabilize net handles and http ownership`

Notable implementation changes:

- stable `NetConn`/`NetListener` handle ids in `rt_net_handles.*`;
- canonical lookup before net entrypoint field access;
- owner-local and fd-generation checks before conn read/write/wait/close;
- `stdlib/http/accept.sg` split;
- HTTP raw `TcpConn.__opaque` worker handoff removed;
- `runtime-v2-net-handle-check` and `runtime-v2-http-owner-check` wired into
  `runtime-v2-check`.

## Verified Gates

- `make runtime-v2-http-owner-check`
- `go test ./internal/vm -run TestMTCorrectnessHTTPServer -count=1`
- `make runtime-v2-net-handle-check`
- `make runtime-v2-accept-check`
- `make c-check`
- `git diff --check`
- commit hook `make check`
- Sentrux `/runtime`: `quality_signal=5345`, bottleneck `redundancy`, rules
  pass, `0` violations.

## LOC State

- `stdlib/http/server.sg`: 475 lines after extracting `accept.sg`.
- `stdlib/http/accept.sg`: 52 lines.
- Commit hook file-size check reported 100% OK for changed runtime native
  files; `rt_net.c` remains within the normal OK band at the current checker
  threshold.

## Known Non-Debt Notes

- `accept_timeout_ms` is an executor timeout. It should not be used as a
  wall-clock readiness/lifetime primitive for external test clients.
- Stable handle ids are intentionally monotonic and not reused. This favors
  stale-copy safety and is acceptable while Surge-visible `NetConn` objects do
  not have a destructor/free path.
- Cross-platform architecture direction is preserved: public Surge handles do
  not expose Linux fd numbers; Linux-specific listener/poller details stay
  inside native backend code.

## Handoff

The next planning pass can choose between:

- entering the explicit Phase 4 crossing/resource-migration discussion, with a
  mandatory language-syntax review first; or
- closing more test/backend-matrix debt (`RV2-DEBT-001`, `RV2-DEBT-002`,
  `RV2-DEBT-011`, `RV2-DEBT-018`) before adding new semantics.
