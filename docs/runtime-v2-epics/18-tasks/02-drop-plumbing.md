# Epic 18 Task 2: Drop Dispatch Plumbing + No-Remote-Owner Rows (1-9)

## Design (locked before rows)

### The single-drop-site reduction

Instead of sprinkling drops over every abandon edge, the pending owns
ONE central drop site and ONE ownership flag:

- `state_drop_fn_id` (u64, 0 = nothing to drop) and `state_owned`
  (set iff the pending currently owns a droppable state) live on both
  pending families (`rt_remote_spawn_pending`,
  `rt_remote_task_pending`).
- The ONLY drop call sits in the FINAL release (refs 1->0): if
  `state_owned && state_drop_fn_id != 0 && state != NULL`, call
  `__surge_drop_call(id, state)` and null the pointer. Every abandon
  edge in matrix rows 1-9 already funnels into the final release —
  the rows prove each edge reaches it exactly once, not that each
  edge remembered to drop.
- The HANDOFF edge clears the flag: when the dispatch successfully
  binds the state to a created body task, `state_owned` drops to 0
  (the body family owns from there — Task 3's rows). Failure paths
  after creation but before publish RESTORE the flag before releasing
  (row 13/14 boundary, proven in Task 3).
- The harness (C rows) provides `__surge_drop_call` as a counting
  stub; compiled programs get the compiler-emitted dispatch in Task 4.

### Runtime surface

- `rt_async_internal.h`: `extern void __surge_drop_call(uint64_t id,
  void* state);` next to `__surge_poll_call`.
- Every standalone C harness that stubs `__surge_poll_call` gains a
  `__surge_drop_call` stub in the same sweep (the Epic 15
  stub-isolation harness-class gotcha, applied proactively).
- Caller sides (`rt_remote_spawn`, `rt_immediate_on_execute*`,
  `rt_far_channel_select`) accept the drop-fn id and set
  `state_owned` at request creation.

### Row-to-mode map (behavior suite, `remote_task_behavior_drop.c`)

Rows 1-9 drive each no-remote-owner terminal edge with a counting
drop stub and assert count==1 (+ pointer nulled, census clean):
destination-shutdown-at-call-site, invalid placement, initial-enqueue
refusal (queue-full via flooded destination), stale anchor, caller
teardown (release_owned sweep, unbound), shutdown/stale discard,
abandoned far Task handle mid-publish, plus the negative control:
id 0 states never invoke the stub (today's shape stays free).
Rows 1-2 (construction/alloc failure) are compile-side or
alloc-injection edges: covered structurally (the drop site is the
final release, which those paths share) and recorded as such.

## Status

COMPLETE. Execution record: the caller surfaces
(`rt_remote_spawn_publish[_placement]`, `rt_immediate_on_execute`,
`rt_immediate_on_execute_anchored`, `rt_far_channel_select`) gained the
`state_drop_fn_id` parameter with pre-pending refusal drops; both
pending families carry the obligation to the single final-release drop
site; the handoff clears `state_owned` at successful body publish in
all three dispatchers; the backend emits the `__surge_drop_call`
dispatch unconditionally (default panic; id 0 never dispatches) and
every standalone harness carries the counting/plain stub.

Rows green: invalid placement, stale anchor, synchronous queue-full
refusal (the corrected row-5 premise — flooded destination data lane),
mixed-owner select refusal, caller-teardown-unbound is deferred to the
Task 3 batch (its bound branch needs the task-side obligation), plus
two negative controls (successful handoff never drops via the pending;
id-0 states never reach the dispatch). Structural coverage note: the
destination-shutdown and allocation-failure refusals share the same
pre-pending drop helper lines the proven rows exercise; a
worker-clobber race makes a direct shard-shutdown row nondeterministic
in this harness (park_state is worker-owned), recorded here instead of
a flaky row.
