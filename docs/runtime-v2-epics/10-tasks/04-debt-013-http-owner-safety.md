# Epic 10 Task 4: RV2-DEBT-013 - HTTP Owner-Safety

**Status:** complete.
**Kind:** stdlib HTTP implementation + Runtime V2 tests, no public crossing
syntax, no Phase 4 transport.

## Starting Hazard

`stdlib/http/server.sg` previously accepted a `TcpConn`, extracted
`conn.__opaque`, sent the raw integer through `Channel<int>`, and had a
pre-spawned worker reconstruct `TcpConn { __opaque = handle }`. That defeated
the Runtime V2 owner-shard contract: the worker placement was decided at
server startup, while the accepted connection owner is decided by the accept
member that produced the fd.

The old flow was also an attractive nuisance for future code because it
laundered an `@nosend` `TcpConn` through an integer channel.

## Result

`http.serve` now uses fixed local accept workers instead of a raw connection
handle queue:

- `serve` creates `worker_count` local accept workers with copied listener
  handles.
- each worker calls `net.accept(&listener)` and handles the returned
  `TcpConn` directly with `serve_conn`;
- no `TcpConn.__opaque`, `Channel<int>`, or `TcpConn { __opaque = ... }`
  handoff remains in the stdlib HTTP server path;
- timed read/write paths use the `Task<T>` returned by `net.read_some` /
  `net.write_all` directly in `timeout(...)`, avoiding extra scheduled
  `spawn net.*` wrapper tasks.

This keeps accepted connection work on the owner-local task path available in
the current runtime. It does not add public cross-shard APIs, remote handler
submission, inbound queues, or distributed cancellation.

## Important Timeout Note

Native Runtime V2 timers are executor timers, not a reliable wall-clock server
lifetime for external Go clients. During task verification, using
`accept_timeout_ms` as a test shutdown mechanism let the executor timeout the
accept workers before the external client connected. The behavior gate therefore
uses `accept_timeout_ms = 0`, proves real HTTP responses, and terminates the
test process from the harness.

The stdlib behavior with finite `accept_timeout_ms` remains defined as a server
idle/accept timeout, not as a test harness readiness primitive.

## Files

- `stdlib/http/accept.sg`
  - new internal accept-worker implementation;
  - no public API.
- `stdlib/http/server.sg`
  - raw `Channel<int>` worker handoff removed;
  - accept worker spawning and shutdown logic kept under the normal `serve`
    function;
  - file reduced below the Runtime V2 LOC limit after splitting accept glue.
- `stdlib/http/http.sg`
  - `copy_listener` helper for internal listener-handle copies.
- `internal/vm/runtime_v2_http_owner_test.go`
  - static gate for the no-raw-handoff contract;
  - behavior gate for `SURGE_SHARDS=1,2,8`.
- `Makefile`
  - `runtime-v2-http-owner-check` added and wired into `runtime-v2-check`.

## Proof

- `TestRuntimeV2HTTPOwnerLocalStaticShape`
  - rejects `Channel<int>`, `Channel::<int>::new`, `conn.__opaque`,
    `TcpConn = { __opaque`, and `spawn net.*` in the HTTP owner path;
  - requires `serve_accept_worker`, copied listener handles, and local accept
    workers.
- `TestRuntimeV2HTTPOwnerLocalBehavior`
  - starts a real `stdlib/http` server;
  - drives HTTP requests at `SURGE_SHARDS=1`, `2`, and `8`;
  - asserts `200` + body `ok` for each client.
- Existing `TestMTCorrectnessHTTPServer` still passes with explicit
  `worker_count` and `accept_timeout_ms` fields in `ServerConfig`.

## Gates

- `make runtime-v2-http-owner-check`
- `go test ./internal/vm -run TestMTCorrectnessHTTPServer -count=1`
- `make runtime-v2-net-handle-check`
- `make runtime-v2-accept-check`
- `make c-check`
- `make check` via commit hook for `9d1b06c1`
- `git diff --check`
- Sentrux `/runtime`: `quality_signal=5345`, rules pass, `0` violations during
  the post-Task-4 verification pass.

## Closeout

`RV2-DEBT-013` is closed. The stdlib HTTP server path is owner-safe for the
current Runtime V2 contract and explicitly tested under multi-shard native
runtime configuration. Future Phase 4 work may add remote handler submission or
explicit resource migration, but this task deliberately does not predesign that
surface.
