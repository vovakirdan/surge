# Epic 6 Task 3: Listener Model Proving Spike

Status: complete with process exception. The subagent reported that the initial
Rule-1 record was written before any spike code, but it implemented and
committed the task before explicit main-agent approval. The main session
therefore treats this as post-facto audited spike evidence: the proof was rerun
from the current checkout before the decision was accepted.

## Hypothesis

Candidate A, per-shard `SO_REUSEPORT` listener group, should be the Epic 6
target if this machine can bind one listener fd per shard to the same
loopback address/port and the kernel accepts high-connection bursts on more
than one listener member. The public `TcpListener` remains one user-visible
handle, but internally it owns a listener group with one member per shard.
Each member has `owner_shard = member_index`. The runtime creates one internal
accept-side system task or poller callback per listener member and places it
on that member's owner shard after Task 6/7 provide real shard placement.
Accepted `NetConn` values inherit the accepting member's `owner_shard`.
Handler placement is runtime-internal: the accepted continuation is scheduled
through an owner-shard spawn/enqueue path, not through new Surge syntax.

Candidate B, explicit handoff fallback, should remain a documented fallback if
a single acceptor can assign accepted fds to target shards before the fd is
registered or exposed as a connection. It is not the ideal hot path because it
centralizes accept and requires an internal one-time placement primitive. If
the only way to make it work is to migrate an already registered connection
or to use Phase 4 cross-shard messages, it fails this epic's boundary.

## Allowed Surfaces

- Create a quarantined scratch probe under
  `build/tmp/runtime-v2-epic6/listener_model_probe.c`.
- Compile and run the scratch binary under `build/tmp/runtime-v2-epic6/`.
- Update this document, `06-evidence.md`, `NOTES.md`, and `DEBT.md` if the
  spike discovers durable follow-up debt.
- Read existing runtime code and docs.

No durable runtime/native, VM, parser, semantic-analysis, lowering, stdlib,
CI, Makefile, or public example file may change in this task.

## Non-Final Behavior

- Hard-coded probe shard/client counts.
- The probe models owner placement with arrays and thread labels only.
- No fd-registry migration, no real scheduler placement, no real per-shard
  poller, no cancellation/shutdown lifecycle, and no trace counters.
- No Phase 4 inbound queues, eventfd, credits, seq-cst `PARKED` protocol, or
  cross-shard messaging.
- No public Surge syntax, standard-library signature, or native ABI change.

## Proof

Compile:

```bash
mkdir -p build/tmp/runtime-v2-epic6
cc -D_GNU_SOURCE -std=c11 -O2 -Wall -Wextra -Werror -pthread \
  build/tmp/runtime-v2-epic6/listener_model_probe.c \
  -o build/tmp/runtime-v2-epic6/listener_model_probe
```

Probe Candidate A:

```bash
build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1
build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 8
build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 32
build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1024
```

Probe Candidate B:

```bash
build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 32
build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 1024
```

Hygiene:

```bash
git diff --check
```

## Success Criteria

- Candidate A binds all requested listener members to one loopback port with
  `SO_REUSEPORT` and `SO_REUSEADDR`.
- Candidate A accepts all requested client connections and the high-load row
  (`1024`) shows more than one active listener member.
- Low-load skew for `1`, `8`, or `32` clients is recorded as expected, not as
  failure.
- The chosen model names where internal accept work lives and how handler
  work reaches the accepted connection's owner shard without syntax changes.
- The public Surge source, stdlib signatures, and native ABI stay unchanged.

## Failure Criteria

- Candidate A cannot bind a same-port listener group on this machine.
- Candidate A accepts all high-load connections on only one listener member.
- Handler placement requires new Surge syntax or Phase 4 cross-shard
  messaging.
- Candidate B requires migration of an already registered/exposed connection
  instead of one-time initial owner placement.
- Any result leaves the internal accept representation or handler owner
  placement as `TBD`.

## Rollback Or Rewrite

The scratch probe is not implementation and must not be promoted into Task 9.
It stays under ignored `build/tmp/runtime-v2-epic6/` or is deleted. If any
idea from it becomes implementation, Task 9 must rewrite it into owner-first
runtime C APIs with explicit status codes and real tests.

## Results

Compile passed:

```bash
cc -D_GNU_SOURCE -std=c11 -O2 -Wall -Wextra -Werror -pthread \
  build/tmp/runtime-v2-epic6/listener_model_probe.c \
  -o build/tmp/runtime-v2-epic6/listener_model_probe
```

Candidate A, `SO_REUSEPORT` listener group:

| Command | Result |
| --- | --- |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1` | `counts=0:0,1:0,2:0,3:1 active_shards=1`, accepted `1/1`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 8` | `counts=0:2,1:3,2:2,3:1 active_shards=4`, accepted `8/8`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 32` | `counts=0:9,1:7,2:8,3:8 active_shards=4`, accepted `32/32`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1024` | `counts=0:241,1:245,2:265,3:273 active_shards=4`, accepted `1024/1024`. |

Candidate B, explicit handoff fallback:

| Command | Result |
| --- | --- |
| `build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 32` | Target assignment `0:8,1:8,2:8,3:8`, accepted `32/32`; requires `initial_owner_placement_before_fd_registry_exposure`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 1024` | Target assignment `0:256,1:256,2:256,3:256`, accepted `1024/1024`; `phase4_required=no_if_one_time_initial_placement yes_if_after_registration_migration`. |

Low-connection skew is expected: the one-client row necessarily activates one
listener member. The 8/32 rows distributed across all four members on this
machine, but Task 12 must not rely on that for correctness because
`RUNTIME_V2.md` already warns that low-count `SO_REUSEPORT` rows can skew.

The probe is 296 lines and remains quarantined under `build/tmp/`; it is not
committed implementation.

## Post-Facto Main-Agent Audit

After the unexpected subagent commit, the main session stopped open agents,
confirmed the tracked tree contained no Task 4/5 files, checked that the scratch
probe remained ignored under `build/tmp/`, and reran the proof from the current
checkout:

| Command | Audit result |
| --- | --- |
| `timeout 60s cc -D_GNU_SOURCE -std=c11 -O2 -Wall -Wextra -Werror -pthread build/tmp/runtime-v2-epic6/listener_model_probe.c -o build/tmp/runtime-v2-epic6/listener_model_probe` | Passed. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1` | Passed; `counts=0:1,1:0,2:0,3:0 active_shards=1`, accepted `1/1`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 8` | Passed; `counts=0:2,1:0,2:4,3:2 active_shards=3`, accepted `8/8`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 32` | Passed; `counts=0:8,1:8,2:6,3:10 active_shards=4`, accepted `32/32`. |
| `timeout 60s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1024` | Passed; `counts=0:259,1:256,2:245,3:264 active_shards=4`, accepted `1024/1024`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 32` | Passed; target assignment `0:8,1:8,2:8,3:8`; accepted `32/32`. |
| `timeout 60s build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 1024` | Passed; target assignment `0:256,1:256,2:256,3:256`; accepted `1024/1024`. |
| `git diff --check` | Passed with no output. |

## Decision

Choose Candidate A, per-shard `SO_REUSEPORT` listener group, as the Epic 6
target.

The internal accept representation is not an always-running user-visible
handler task. It is a listener-group object with one listener member per shard:

- `listener_group.members[k].fd` is registered in shard `k`'s fd registry;
- each member has `owner_shard = k`;
- `rt_net_wait_accept()` on the public listener group registers the waiting
  accept task against all live member fds or an equivalent group wait record;
- the owner shard's net poller completes the winning member's accept readiness;
- the accept waiter is resumed/enqueued on the winning member's owner shard;
- `rt_net_accept()` accepts from that winning member and creates a `NetConn`
  whose `owner_shard` equals the member owner.

That makes handler placement concrete without new syntax: code that awaits
`net.accept(&listener)` resumes on the accepted connection owner shard, so a
local `spawn` issued from that continuation stays owner-local. This is an
Epic 6 runtime-internal accept-resume rule, not Phase 4 cross-shard messaging:
the executor lock remains global, the waiter is already a runtime task, and no
user-level `submit_to`/`far` syntax is introduced. Task 4/5 must encode this
as a contract before Task 7/9 implementation.

The public Surge source and stdlib signatures stay unchanged:
`net.listen(addr, port) -> NetResult<TcpListener>` and
`net.accept(&listener) -> Task<NetResult<TcpConn>>` remain the public surface.
The native `rt.h` function names and return ABI remain stable. Internal native
`NetListener`/`NetConn` representation may grow owner/group metadata in Task 8.

Candidate B remains a fallback only. It can assign target shards in a probe if
the fd is placed before registry exposure, but it centralizes accept on shard 0
and becomes Phase 4-style migration if it moves an already registered/exposed
connection. Task 9 must not implement this fallback unless Candidate A hits a
new concrete blocker; if it ever does, Task 12 trace/benchmark evidence must
label it as a compatibility handoff, not the target hot path.

## Follow-Up Debt

Current `stdlib/http/server.sg` sends raw `TcpConn.__opaque` handles through a
channel to worker tasks before read/write. Under `SURGE_SHARDS>1`, that worker
may not be the accepted connection's owner shard. This does not block choosing
the listener model, but it must not be forgotten: `RV2-DEBT-013` records the
stdlib owner-placement follow-up, and Task 7/9/13 must make non-owner use
visible through guards or tests.
