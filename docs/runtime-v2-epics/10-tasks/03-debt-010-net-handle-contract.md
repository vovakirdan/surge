# Epic 10 Task 3: RV2-DEBT-010 - Stable Net Handle Contract

**Status:** complete.
**Kind:** native runtime net safety, no stdlib signature change, no syntax, no
Phase 4 transport.

## Result

`TcpConn.__opaque` and `TcpListener.__opaque` now carry stable runtime handle
ids. They are not OS file descriptors and not native pointers. Full native
objects (`NetConn`, `NetListener`) and reconstructed Surge boxes both start
with the same handle-id word; every native net entrypoint must canonicalize
that word through the runtime handle table before reading fields beyond byte 8.

This closes the old copied-handle hazard without depending on Linux fd reuse
behavior. A copied stale handle whose native object was closed is removed from
the handle table and fails with `NET_ERR_NOT_CONNECTED` before it can attach
readiness interest or issue `read(2)`, `write(2)`, `accept(2)`, or `close(2)`
against a reused backend fd.

## Written Model

- **Owned state and lifetime changed:** `runtime/native/rt_net_handles.c` owns
  a process-local handle table keyed by monotonic `uint64_t` ids. Entries are
  typed (`NET_HANDLE_CONN`, `NET_HANDLE_LISTENER`) and point at canonical
  runtime objects. Ids are not reused.
- **Public/internal contract before the task:** copied/reconstructed public
  net handles carried only the first word of the native object. Earlier data
  paths could read raw fd/closed state directly from stale or 8-byte copied
  boxes.
- **Old unsafe path:** a stale copied `TcpConn` could conceptually operate on
  an fd number after the original connection had closed and the OS had reused
  that number. A copied 8-byte box also made any read of fields beyond the
  public handle word out-of-bounds.
- **New invariant:** public net entrypoints first resolve the handle id to a
  canonical `NetConn`/`NetListener`; only that canonical object exposes fd,
  owner shard, closed state, and fd-registry generation. Conn read/write/wait
  and close additionally validate owner-local execution and the owner shard's
  fd-registry generation with `rt_net_handle_open_on_owner`.
- **Failing proof if regressed:** stale copied handles must return
  `NET_ERR_NOT_CONNECTED`, not park on or close a newer connection. Static
  tests must fail if data-path ops stop calling the canonical guard.

## Implementation Surfaces

- `rt_net_handles.h/.c`
  - `NetListener` and `NetConn` start with `uint64_t handle_id`.
  - `net_handle_registry_add/lookup/remove` own stable handle ids.
  - `rt_net_listener_canonical(_const)` and `rt_net_conn_canonical(_const)`
    resolve copied handles before field access.
  - `rt_net_conn_registry_remove` removes a closed connection handle.
- `rt_net.c`
  - `net_conn_from_borrowed/value` and `net_listener_from_borrowed/value`
    canonicalize through the handle table.
  - `net_conn_op_open` rejects closed, stale, non-owner, or generation-mismatched
    conn operations.
  - `rt_net_connect` and `rt_net_accept` stamp the fd-registry generation into
    the canonical `NetConn`.
  - `rt_net_close_conn` removes the handle-table entry only after the owner
    close succeeds.
  - `rt_net_close_listener` removes listener registry and handle-table state
    after closing listener members.
- `rt_net_accept_group.c/.h`, `rt_net_lifecycle.c/.h`, `rt_fd_registry.c/.h`
  - owner-locked fd generation checks remain the lifetime proof for live
    canonical handles.
  - the removed 16-bit public-handle predicate is not part of the final
    contract.

## Behavior Matrix

| Operation on copied handle | Live canonical object | After close | After close + backend fd reuse |
| --- | --- | --- | --- |
| `read`/`write`/`read_bytes`/`write_bytes` | owner-local + generation validated | `NotConnected` | `NotConnected`; no syscall on reused fd |
| wait readable/writable | owner-local + generation validated before interest registration | resumes and next op fails | no interest attaches to the reused fd row |
| `close_conn` | one owner close succeeds | `NotConnected` | stale close cannot close the newer fd |
| `accept` on copied listener | canonical listener object | `NotConnected` after removal | stale listener id fails canonical lookup |

## Proof

- `TestRuntimeV2NetHandleStaleCopyReusedFD`
  - runs with `SURGE_SHARDS=1,2,8`;
  - stale read/write/close each return code `5` (`NET_ERR_NOT_CONNECTED`);
  - stale close does not break the newer accepted connection's probe write.
- `TestRuntimeV2NetHandleGuardStaticShape`
  - pins the handle-id contract text;
  - requires the stable handle table and canonical conn lookup;
  - rejects the removed 16-bit public-handle predicate.
- `runtime-v2-net-handle-check`
  - wired into `runtime-v2-check`.

Negative control from the pre-guard fixture hangs by parking a stale read on a
reused fd's readiness; the current implementation exits cleanly.

## Residual Boundaries

- The handle table grows monotonically and ids are not reused. This is
  deliberate for stale-copy safety and no worse than the current lack of a
  Surge-visible `NetConn` free path.
- The fd-registry generation check still matters for live canonical handles:
  it protects validate-vs-close ordering under the owner shard lock.
- This task does not implement Phase 4 resource migration. Non-owner conn use
  under `SURGE_SHARDS>1` is rejected with `NET_ERR_NOT_CONNECTED` unless the
  current worker/task is already owner-local.

## Gates

- `make runtime-v2-net-handle-check`
- `make runtime-v2-accept-check`
- `make c-check`
- `make check` via commit hook for `9d1b06c1`
- `git diff --check`
- Sentrux `/runtime`: `quality_signal=5345`, rules pass, `0` violations during
  the post-Task-4 verification pass.
