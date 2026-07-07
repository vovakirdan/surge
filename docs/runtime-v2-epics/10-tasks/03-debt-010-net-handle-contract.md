# Epic 10 Task 3: RV2-DEBT-010 — Copied Net-Handle Generation Contract

**Status:** implemented; the design was revised mid-task by the handle-word
discovery below.
**Kind:** runtime code (native net + fd registry), no stdlib signature change,
no syntax, no Phase 4 transport.

## Load-bearing discovery: the 8-byte handle word

The original plan stamped a full 64-bit generation into `NetConn`. Tracing a
guard rejection (`net-guard-reject fd=6 ... gen=0` on a pointer that
`rt_net_conn_alloc` never returned) exposed the real public-handle ABI:

- Surge lowers struct values as heap-boxed pointers, and the `NetResult`
  payload `NetConn*` is used directly as the `TcpConn` box pointer, so
  `conn.__opaque` reads the FIRST 8 BYTES of `NetConn` (packed
  `{fd:int32, closed:u8, owner_shard_valid:u8, 2 pad bytes}`), not a pointer.
- `TcpConn { __opaque = handle }` reconstruction allocates a fresh 8-byte box
  holding only that prefix. C code that dereferences a reconstructed handle
  beyond byte 8 (`owner_shard_id`, any new 64-bit field) reads OUT OF BOUNDS —
  pre-existing UB that `rt_net_close_conn` already committed on stdlib HTTP's
  reconstructed conns. This is precisely what the DEBT-010 ledger phrase
  "copied net handles still carry only the raw native fd view" meant.
- `NetListener` survives copying only because every operation resolves the
  canonical full struct through the fd-keyed listener registry.
- `__opaque` values are raw words, not bignum ints: Surge programs may only
  move them; arithmetic or comparison on them crashes in `rt_bigint_*`.

Consequences: the conn guard may trust ONLY the handle word; the spare two
padding bytes become a 16-bit `generation_check`; and the owner shard must be
resolved by a registry probe, never read from the struct.

## Written model (epic Proof And Quality Contract)

- **Owned state and lifetime being changed:** `NetConn` gains a 16-bit
  `generation_check` inside the 8-byte handle word (previously padding), and
  `NetListenerMember` gains a full 64-bit `generation`; both are stamped from
  the owning shard's fd-registry row at registration time. The fd-registry
  row (mutated only under the owner shard lock) remains the single source of
  truth; the stamps are write-once before the handle is published, so reading
  them from any alias or reconstruction is race-free.
- **Public/internal contract before the task:** `TcpConn.__opaque` /
  `TcpListener.__opaque` is a raw `NetConn*`/`NetListener*` shared by every
  copy. Public data-path ops (`rt_net_read`, `rt_net_write`,
  `rt_net_read_bytes`, `rt_net_write_bytes`, `rt_net_accept`,
  `rt_net_close_conn`, `rt_net_close_listener`, `rt_net_wait_readable`,
  `rt_net_wait_writable`) validate only the in-struct `closed` bool — a plain,
  unsynchronized field — and then issue the raw syscall on `c->fd`.
- **Old unsafe path:** (a) a copied handle can race the unsynchronized
  `closed=true` / `fd=-1` writes of a concurrent close and issue `read(2)` /
  `write(2)` on an fd number the OS has already reassigned to a different
  socket; (b) two concurrent closes of the same conn both pass the `closed`
  check, and the second `rt_fd_registry_mark_closed` finds no row and returns
  OK, so both call `close(2)` — the second close can tear down an unrelated
  reused fd; (c) a stale waiter can attach poll interest to the NEW row of a
  reused fd and later consume its readiness.
- **New invariant:** every public conn data-path operation (`read`, `write`,
  `read_bytes`, `write_bytes`, `wait readable/writable`, `close`) validates
  the 8-byte handle word against the fd registry through
  `rt_net_conn_probe_open`: the probe visits shard registries (current-worker
  hint first) under each shard's lock, the first REGISTERED row for the fd
  decides, and the row must be OPEN with `generation & 0xFFFF ==
  generation_check`. Accept validates each listener member against its full
  64-bit generation (`rt_net_handle_open_on_owner`) after stamping members at
  registration. Close revalidates under the owner lock before
  `mark_closed`, so exactly one closer wins and a reused fd is never
  `close(2)`'d by a stale handle. Every rejection uses the existing stable
  `NET_ERR_NOT_CONNECTED` status path.
- **Failing proof if regressed:** `TestRuntimeV2NetHandleStaleCopyReusedFD`
  (SURGE_SHARDS=1,2,8): stale read/write/close on a reused fd must each
  return exactly `NotConnected(5)` and the stale close must not kill the
  reusing connection's probe round-trip; `TestRuntimeV2NetHandleGuardStaticShape`
  pins the guard wiring. Negative control: the same fixture built at the
  pre-guard commit `ec7721c5` HANGS (timeout kill, exit 143) — the stale read
  parks on the reused fd's readiness, exactly the waiter-consumption hazard
  the ledger described. Both wired into `make runtime-v2-net-handle-check`
  inside `runtime-v2-check`.

## Why a registry fd-index accompanies the guard (Global Rule 5 record)

`rt_fd_registry_find_const` is a linear scan; every live connection holds a
row, so a 1-shard/1024-connection workload would pay O(1024) per read/write —
an unacceptable hot-path regression for a safety check. The existing primitive
is therefore insufficient for a per-op guard. The registry gains an
`fd -> entry index` dense array (`int32_t* fd_index`), owned by the same
registry struct, maintained at exactly the two existing mutation points
(`fd_registry_create_row`, `fd_registry_remove_at`), making `find` O(1).
Cancellation/shutdown/error paths are unchanged: the index is derived state
under the same owner shard lock. It also speeds the existing wait-path probe
and attach/detach scans. Proof: existing fd-registry gates must stay green;
the net benchmark must not regress materially.

## Copied-handle behavior matrix (contract this task defines and tests)

| Operation on a copy | Live handle | After close (any alias) | After close + fd reused |
| --- | --- | --- | --- |
| `read`/`write`/`read_bytes`/`write_bytes` | unchanged | `NotConnected` | `NotConnected` (16-bit check mismatch, locked probe) |
| `close` | closes once | `NotConnected` | `NotConnected`; never a second `close(2)` |
| `wait readable/writable` | unchanged | proceed-and-fail via the op guard | no interest attach on the new row |
| `copy`/reconstruct (`{__opaque}`) | carries the handle word | same as above | same as above |
| accept on listener member | unchanged | `NotConnected` | member guard rejects mismatched row (full 64-bit) |

The conn check is 16-bit by ABI necessity (only two padding bytes exist in
the handle word), so a stale handle validates falsely only if the same fd
number is re-registered on the same shard exactly k·65536 generations later
while the stale copy is still held — a bounded ~2^-16 aliasing residual per
reuse event, recorded in `DEBT.md` as part of the close note. `NetConn`
allocations are never freed today (pre-existing accepted leak; no destructor
exists on the Surge side), so aliases cannot dangle; this task documents that
fact and does not change it.

## Narrowed listener boundary (documented, not hidden)

Listener copies resolve through the canonical fd-keyed listener registry. A
listener copy racing a concurrent `close_listener` while a new listener binds
the same fd number can canonicalize to the NEW listener object and accept from
it. Full listener-handle generation canonicalization is deliberately out of
scope: the accept-time member guard added here validates the fd row before
`accept(2)`, and the residual object-identity race requires concurrent close +
rebind + a torn read of the copy's fd field. Recorded as a named narrowed
boundary in `DEBT.md` rather than silently accepted.

## Implementation surfaces

- `rt_fd_registry.h/.c`: `fd_index` dense map; `rt_fd_registry_handle_open`
  (guard predicate: row exists && `registered_open` && OPEN && generation
  equal); `rt_fd_registry_register_open_fd_generation` (register + report the
  row generation).
- `rt_net_handles.h/.c`: `generation` fields; `rt_net_conn_alloc` and
  `rt_net_listener_set_member` gain the generation parameter.
- `rt_net_accept_group.c`: `rt_net_register_open_fd_on_owner` reports the row
  generation; listener-member registration stamps member generations.
- `rt_net_lifecycle.c`: `rt_net_close_fd_on_owner` validates the handle
  generation under the owner shard lock before `mark_closed`; row-absent close
  stays legal only for never-registered members.
- `rt_net.c`: owner-locked guard helper + calls in read/write/read_bytes/
  write_bytes/accept/close/wait entry points.
- Tests: `internal/vm/runtime_v2_net_handle_guard_test.go` (+ static shape),
  new Makefile stage `runtime-v2-net-handle-check` wired into
  `runtime-v2-check`.

## Gates

`git diff --check`, `make c-check`, `make cppcheck`, focused new tests,
`make runtime-v2-check`, `make check`, `./check_file_sizes.sh -a`, Sentrux
root + `runtime/` + `runtime/native`, and `scripts/bench_native_net.sh`
before/after rows (1-shard/1024 and 8-shard/1024, direct/pipe) with trace
counters explaining any delta.
