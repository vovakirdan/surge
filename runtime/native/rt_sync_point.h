// Runtime V2 proving spike: test-only deterministic interleaving hooks.
//
// RT_SYNC_POINT(name) and RT_SYNC_POINT_IF(cond, name) mark an instruction-scale
// rendezvous inside REAL runtime code so a test can pause one thread at a named
// window and release it in lockstep with another, reproducing a few-instruction
// ordering window deterministically (see docs/runtime-v2-epics/09-tasks/
// 01-proving-spike-sync-points.md). Unless RT_TEST_SYNC_POINTS is defined the
// macro emits no call, no branch, and no symbol on the shipping path. That
// property is enforced mechanically by check_sync_points.sh (nm of the
// default/tag-off build must show zero rt_sync_point_* symbols).
//
// Allowlist: `name` must be one of the RT_SYNC_POINT_* enumerators below or the
// translation unit fails to compile, in BOTH the armed and the release build
// (the release macro still references the enumerator). Adding a site therefore
// requires an enumerator here, a name row in rt_sync_point.c, and a designated
// window in check_sync_points.sh.
#ifndef RT_SYNC_POINT_H
#define RT_SYNC_POINT_H

// The allowlist. Always defined (a pure compile-time enum; it emits no runtime
// symbol), so the release macro can reference it to enforce the allowlist
// without linking any rendezvous code.
typedef enum rt_sync_point_id {
    RT_SYNC_POINT_NONE = 0,
    // cancel_task: target still RUNNING immediately before the unconditional wake; pairs with
    // SP_PARK_BEFORE_WAITING to reproduce the RV2-DEBT-023 cancel-vs-park race.
    RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE,
    // user poll returned PARKED, before TASK_WAITING is committed while still RUNNING.
    RT_SYNC_POINT_SP_PARK_BEFORE_WAITING,
    // park_current's first wake-token exchange returned zero; no park is committed yet.
    RT_SYNC_POINT_SP_PARK_AFTER_INITIAL_TOKEN_CHECK,
    // second token abort requeued READY and unlocked; waiter removal has not begun.
    RT_SYNC_POINT_SP_PARK_ABORT_AFTER_REQUEUE,
    // mark_done tail: reached after the TASK_DONE store, immediately before the
    // post-DONE seq-cst done_waiters load. Pairs with
    // SP_AWAIT_AFTER_INCREMENT to reproduce the RV2-DEBT-022 StoreLoad window.
    RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD,
    // rt_task_await: reached after the done_waiters increment, before the
    // seq-cst status predicate load.
    RT_SYNC_POINT_SP_AWAIT_AFTER_INCREMENT,
    // rt_task_await: reached after the external awaiter observed not-DONE and
    // immediately before pthread_cond_wait atomically releases ex->lock. Pairs
    // with RV2_DEBT_022_NEGATIVE_CONTROL to prove unlocked done_cv broadcasts
    // can be lost when they race this final wait transition.
    RT_SYNC_POINT_SP_AWAIT_BEFORE_DONECV_WAIT,
    // rt_task_poll: reached after prepare_park has published the current
    // task's JOIN waiter and pending_key records that park, before the target
    // DONE re-check. Tests can now cancel a proven registered joiner without
    // relying on scheduler timing.
    RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER,
    // blocking task poll: reached after observing a pending blocking job but
    // before publishing its blocking-key waiter. The blocking worker can
    // complete and drain the empty key while this point is held, proving the
    // post-registration terminal re-check closes the lost-completion window.
    RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER,
    // wake_key_all_with_policy: reached inside the batch drain loop, so a cancel
    // can race a mid-drain key wake (token vs batch-compaction ordering).
    RT_SYNC_POINT_SP_WAKEKEY_MID_DRAIN,
    // rt_task_replace_owner / join-waiter migration: reached after the owner
    // replacement route publication boundary. The positive build publishes the
    // join route before this point; the RV2_DEBT_020_NEGATIVE_CONTROL build
    // reaches it before publishing, reproducing the old migrate gap.
    RT_SYNC_POINT_SP_MIGRATE_GAP,
    // transport consumer: reached after the inbound drain, before publishing
    // the shard PARKED state.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK,
    // transport consumer: reached after publishing PARKED, before the required
    // inbound re-check that prevents parked-with-work strands.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK,
    // transport producer: reached after publishing the complete message,
    // before reading the target shard's park state.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD,
    // transport producer: reached after reading park state, before deciding
    // whether to write the transport wake fd or elide the wake.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE,
    // transport reply waiter: reached before a task suspends on a reply waiter;
    // this must not be a shard park.
    RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND,
    // transport shutdown: reached after shutdown is visible, before waking all
    // transport-parked shards and reply waiters.
    RT_SYNC_POINT_SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE,
    // remote task owner: reached after observing not-DONE but before
    // publishing the owner reply edge. A test completes in this window to
    // prove register-then-verify closes the lost completion.
    RT_SYNC_POINT_SP_REMOTE_TASK_BEFORE_OWNER_REGISTER,
    // remote task owner: reached after publishing the owner reply edge and
    // pinning the task, before the DONE re-check. A test completes the task in
    // this window to prove both the re-check and the task-lifetime pin.
    RT_SYNC_POINT_SP_REMOTE_TASK_AFTER_OWNER_REGISTER,
    // remote spawn destination: reached at dispatch entry, before the pending
    // snapshot that gates body creation. A test abandons the caller-owned
    // handle in this window to prove a pre-dispatch abandon still hands the
    // shipped state to exactly one owner (the published body) and turns the
    // eventual ack into an owner-routed release.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH,
    // remote spawn destination: reached after the body task exists and the
    // pending carries its handle, before the body is published (the
    // spawn-on twin of SP_IMMEDIATE_ON_BEFORE_PUBLISH). An abandon in this
    // window must neither leak nor double-drop the shipped state.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH,
    // remote spawn destination: reached after the child is published, before
    // its ack is enqueued. Cancellation here must abandon the caller-owned
    // lease and turn the eventual ack into an owner-routed release.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK,
    // immediate on destination: reached at execute-dispatch entry, before
    // the token match and pending snapshot that gate body creation. A test
    // cancels the caller in this window to prove the teardown sweep
    // resolves the UNBOUND request and the late dispatch refuses to create
    // a body (the pending stays the state's sole owner and drops once).
    RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH,
    // immediate on destination: reached after the execute pending is bound to
    // the created body task and owner-registered, before the body task is
    // published. A test cancels the caller in this window to prove the
    // caller-cancel route and the reply edge resolve exactly once.
    RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH,
    // immediate on destination: reached AFTER the body task is published and
    // before the publication-accepted handoff store clears state_owned.
    // Publishing makes the body runnable, so in this window another thread
    // can complete it, consume the owner registration, drop the last pending
    // reference and free the pending -- while this thread is still about to
    // write into it. A test holds the dispatch here, drains the body and
    // resolves the caller, then releases: without the dispatch's own pending
    // reference the store lands in freed memory (RV2-DEBT-061 shape (b)), and
    // the free path can read a stale state_owned and drop body_state a second
    // time (shape (a)).
    RT_SYNC_POINT_SP_IMMEDIATE_ON_AFTER_PUBLISH,
    // ready_push_with_policy (force-inject requeue: yielded tasks, net
    // wakes): reached after the unlocked status/enqueued pre-checks pass,
    // before the owner shard lock. A wake in this window can enqueue the
    // task and hand it to another worker that stores RUNNING; the locked
    // re-validation must then refuse the duplicate push, or its READY store
    // overwrites RUNNING under a live poll and a second worker double-polls.
    RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK,
    // wake_task_with_policy: reached after the owner lock is released with a
    // captured stale park key, before the deferred waiter removal. The woken
    // task can re-poll and re-park on the SAME key in this window; an
    // unqualified removal here sweeps the fresh registration too and strands
    // a store-driven (join) park forever (RV2-DEBT-046).
    RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL,
    // remote select owner body (rt_anchored_channel_select): reached after
    // rt_select_poll has committed a winner (its control-lock critical
    // section already released) and before the winner value returns to
    // the caller's async-return/reply. A test cancels the caller in this
    // window to prove the one-lock commit ships as success even though a
    // cancel races in after the commit (Epic 20 Task 7 row 2).
    RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY,
    // remote select destination dispatch entry (rt_far_channel_dispatch_select):
    // reached before the pending snapshot that gates the arm pin loop, the
    // select twin of SP_IMMEDIATE_ON_BEFORE_DISPATCH. A test cancels the
    // caller in this window to prove the teardown sweep resolves the
    // UNBOUND request and the late dispatch refuses to pin any arm (Epic
    // 20 Task 7 row 3).
    RT_SYNC_POINT_SP_FAR_SELECT_BEFORE_DISPATCH,
    // Hold the first exclusive jumbo reservation, then prove the competing
    // sender parked.
    //
    // The second name says TRANSPORT DATA SLOT and not CARRIER CREDIT, and
    // both halves of that were wrong before. A physical BYTE credit does not
    // exist for pointer transport -- a cross-shard message carries a pointer
    // into a refcount graph the transport neither copies nor owns, so there is
    // no per-message byte cost to charge -- and the budget that does exist is
    // slots. And an async crossing parks the TASK, not the carrier: the
    // carrier goes on to run other work.
    //
    // Nothing arms the second point yet. The admission it names parks a sender
    // on an exhausted data-slot budget, and the tree still drain-and-retries
    // instead, so the site belongs to the far-carrier work that builds the
    // park. A probe waiting on it therefore waits forever rather than failing,
    // which is why the two liveness probes that use it are deferred with that
    // reason rather than left to time out.
    RT_SYNC_POINT_SP_CARRIER_JUMBO_ADMITTED,
    RT_SYNC_POINT_SP_TRANSPORT_DATA_SLOT_TASK_PARKED,
    // rt_sleep_fire_due_on_shard: reached after the due batch has been popped
    // out of the sleep store and the shard lock released, before the first
    // wake puts any of it back. Holding a worker here is the whole of the
    // RV2-DEBT-190 window: the fired sleeper is in no store, no queue and no
    // running count, so an idle sample taken now sees an empty executor and
    // the virtual clock advances past a deadline that has already come due.
    RT_SYNC_POINT_SP_SLEEP_FIRED_BEFORE_WAKE,
    // channel_reclaim_if_unreferenced (rt_channel_refcount.c): reached after
    // the release that retired the last handle or pin has claimed the reclaim,
    // and before the object is handed to the deferred free. A driver holding a
    // thread here while another retains, sends or receives on the same channel
    // is what proves the count -- and not the timing -- is what decides the
    // free (RV2-DEBT-155).
    RT_SYNC_POINT_SP_CHANNEL_LAST_RELEASE_BEFORE_FREE,
    // rt_scope_join_all: reached after the owner registered its scope_key
    // waiter and released the first snapshot (a live child, fail-fast not yet
    // fired), before the verify re-read. A driver cancelling the child while
    // the owner is held here lands the child's completion accounting -- flag
    // set and count retired, under one lock -- inside the window the verify
    // must read whole (RV2-DEBT-261).
    RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY,
    // Cancelled scope teardown: after observing live children and before
    // publishing its scope-key waiter. Draining the final child while held
    // here proves the post-registration verify prevents a lost wake.
    RT_SYNC_POINT_SP_SCOPE_TEARDOWN_BEFORE_REGISTER,
    // rt_async_return: reached after the body's value has been moved into the
    // task's own result slot and committed there, and before the success
    // outcome is handed to the scheduler. A driver cancelling the task while it
    // is held here lands the cancellation strictly after the last suspension
    // point this task will ever have -- so nothing in the poll can still
    // observe it -- and strictly before mark_done chooses the kind to commit.
    // That gap is the whole of RV2-DEBT-263: the commit boundary is the only
    // place left that can still answer Cancelled.
    RT_SYNC_POINT_SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT,
    // mark_done: reached after the cancel gate has been sealed and the kind
    // chosen, and before TASK_DONE is published. This is the RESIDUAL window --
    // everything the completion still has to do lives inside it, including a
    // mutex acquisition in release_matching_leases (rt_remote_task_lease.c) --
    // so it is where a cancel that arrives "just too late" actually lands.
    // Holding a completion here and cancelling it must produce ONE answer:
    // the cancel is refused, its CAS finding the gate sealed, and the task
    // answers Success. Never a task answering Success while its canceller
    // believes it landed (RV2-DEBT-263).
    RT_SYNC_POINT_SP_MARKDONE_AFTER_SEAL_BEFORE_DONE,
    // blocking worker (rt_async_blocking.c): reached after a job has been
    // popped and claimed in the running count, before its status is read. A
    // cancel landing here is "cancel before claim": the worker
    // must observe CANCELLED and release a state it never claimed -- walked
    // through its descriptor, then freed.
    RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS,
    // blocking worker: reached after the state cell has been marked spent and
    // before the body runs. A cancel landing here is "cancel after claim":
    // its CAS wins while the body runs and consumes the captures, so the
    // release that follows must free only the block. The
    // RV2_DEBT_080_WALK_ALWAYS_NEGATIVE_CONTROL build removes the claim and
    // MUST be seen destroying the captures a second time.
    RT_SYNC_POINT_SP_BLOCKING_STATE_BEFORE_BODY,
    // blocking worker: reached with a job popped AFTER the pool's shutdown was
    // published, before that job is cancelled and released instead of run.
    // Holding a worker here is what proves a queued body is drained by
    // cancellation at shutdown and never executed.
    RT_SYNC_POINT_SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN,
    // ready_claim_current_local_tail (rt_ready_queue.c): reached once the task
    // has been removed from this worker's local tail and that shard lock has
    // been released, immediately before the inline poll of it begins. Claiming
    // it -- clearing enqueued, storing RUNNING, consuming the wake token -- is
    // what tells every other thread the task is spoken for, and doing that
    // outside the lock that removed it leaves an instant when the task is in no
    // queue and still reads schedulable. A wake landing there passes the wake
    // path's gate and queues a SECOND entry for a task this thread is about to
    // poll. Holding a worker here and waking the task is what shows whether the
    // take and the claim are one observation.
    RT_SYNC_POINT_SP_INLINE_CHILD_TAKEN_OFF_QUEUE,
    // rt_scope_register_child (rt_async_scope.c): reached once this child's
    // membership has been DECIDED -- the claim has run and answered -- and
    // before the scope's accounting is published for it. A driver that lands a
    // completion in this gap is asking the one question the two racers have to
    // agree on: a child that completes here is either a member the scope will
    // retire, or a non-member whose completion the registration must report
    // itself. Holding the registration here is what makes "both sides decided
    // the other was not there" reproducible instead of one run in hundreds.
    RT_SYNC_POINT_SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH,
    // scope_on_child_done (rt_async_scope.c): reached by EVERY completing task
    // immediately after it has taken its own membership answer out of the claim
    // word and before it acts on it. Never arm this one to block -- every
    // completion in the process crosses it -- it is here to be COUNTED, so a
    // driver can prove a completion really did read its membership inside the
    // window a held registration is holding open, instead of inferring it from
    // a sleep.
    RT_SYNC_POINT_SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE,
    // rt_task_entitlement_begin_take: reached after the take has been decided
    // CLONE, the reader has been counted into clone_readers, and the owner
    // shard lock has been released -- the instant the duplication out of the
    // canonical slot begins, with nothing held. It is the only window in which
    // a claim is OUT: the value must not move, and it must not be destroyed,
    // until this reader retires. Holding a reader here and then doing something
    // to the task from another thread is what shows whether the claim really
    // pins the canonical result.
    RT_SYNC_POINT_SP_CLONE_READER_OUT_OF_LOCK,
    // cancel_task: reached with the target already DONE and its result slot
    // still holding a published value -- a cancel that arrived after the task's
    // answer was decided. The storage model says such a cancel does not revoke
    // results that are already available, so this point exists to prove the
    // cancel ARRIVED there (its reached count) rather than to hold anything
    // open by itself.
    RT_SYNC_POINT_SP_CANCEL_AT_COMMITTED_RESULT,
    // rt_task_result_matches: reached with a result capability resolved to a
    // live task, immediately before the slot is asked whether it still holds
    // the occupant that capability was minted for. Holding a late holder here
    // and retiring plus rebinding the slot underneath it is what shows whether
    // the generation, and not the task id alone, is what answers.
    RT_SYNC_POINT_SP_RESULT_CAPABILITY_BEFORE_MATCH,
    RT_SYNC_POINT_COUNT
} rt_sync_point_id;

#ifdef RT_TEST_SYNC_POINTS

// Armed build only. Reaching an armed point performs its configured rendezvous
// action (barrier / block / open); reaching an unarmed point is a cheap no-op.
void rt_sync_point_reach(rt_sync_point_id id);

// Test-driver helpers (harness-callable, armed build only):
//   rt_sync_point_reached_count  how many times a point was reached this
//                                process, so a proof can assert the window was
//                                actually exercised (never a silent skip).
//   rt_sync_point_open           grant one permit to the shared semaphore,
//                                releasing one thread blocked at a `block`
//                                point. Lets a driver thread order an
//                                interleaving explicitly (block in runtime
//                                code, release from the driver after it has
//                                performed the racing action).
//   rt_sync_point_wait_until_after
//                                block the driver until a point's reached
//                                count exceeds a captured value; returns zero
//                                only on the bounded deadlock guard.
unsigned rt_sync_point_reached_count(rt_sync_point_id id);
int rt_sync_point_wait_until_after(rt_sync_point_id id, unsigned before);
void rt_sync_point_open(void);

#define RT_SYNC_POINT(name) rt_sync_point_reach(RT_SYNC_POINT_##name)
#define RT_SYNC_POINT_IF(cond, name)                                                               \
    do {                                                                                           \
        if (cond) {                                                                                \
            RT_SYNC_POINT(name);                                                                   \
        }                                                                                          \
    } while (0)

#else

// Release/default build. The cast keeps the allowlist load-bearing (an
// unknown `name` still fails to compile) while folding to nothing: no call, no
// symbol, nothing on the steady path.
#define RT_SYNC_POINT(name) ((void)RT_SYNC_POINT_##name)
#define RT_SYNC_POINT_IF(cond, name) ((void)RT_SYNC_POINT_##name)

#endif // RT_TEST_SYNC_POINTS

// RV2-DEBT-023 negative-control toggle (proof infrastructure, available
// in every build so cancel_task compiles either way). Default expands to the
// unconditional wake (the fix). A build that defines RV2_DEBT_023_NEGATIVE_
// CONTROL restores the pre-fix status-gated wake, which MUST strand the
// deterministic cancel-vs-park proof (the non-vacuity check). One statement
// either way, so cancel_task's effective LOC is unchanged by the toggle.
#ifdef RV2_DEBT_023_NEGATIVE_CONTROL
#define RT_DEBT023_CANCEL_WAKE(ex, task)                                                           \
    do {                                                                                           \
        if (task_status_load(task) == TASK_WAITING) {                                              \
            wake_task((ex), (task)->id, 1);                                                        \
        }                                                                                          \
    } while (0)
#else
#define RT_DEBT023_CANCEL_WAKE(ex, task) wake_task((ex), (task)->id, 1)
#endif

// RV2-DEBT-143 negative-control toggle. The fixed blocking poll publishes its
// waiter and then treats a terminal re-check as authoritative. Defining the
// negative control makes that post-registration observation inert, restoring
// the old lost-completion path for the deterministic proof.
#ifdef RV2_DEBT_143_NEGATIVE_CONTROL
#define RT_DEBT143_POST_REGISTER_TERMINAL(status) 0
#else
#define RT_DEBT143_POST_REGISTER_TERMINAL(status)                                                  \
    ((status) == BLOCKING_JOB_DONE || (status) == BLOCKING_JOB_CANCELLED)
#endif

// RV2-DEBT-190 negative-control toggle. The fix counts a collect-then-wake
// batch as in flight from the moment it leaves its structure until it is
// republished, so an idle sample taken in that gap sees work rather than an
// empty executor — which matters because idleness is what the virtual clock
// advances on. Defining the negative control makes the claim inert, restoring
// the state in which the batch is invisible, so the deterministic proof MUST
// observe the executor reporting itself idle with a fired sleeper in hand.
#ifdef RV2_DEBT_190_NEGATIVE_CONTROL
#define RT_DEBT190_PUBLISHING(count) ((size_t)0)
#else
#define RT_DEBT190_PUBLISHING(count) (count)
#endif

// RV2-DEBT-201 negative-control toggle. park_current's FIRST abort branch
// (the wake token was already set when the park began) fires BEFORE
// add_waiter, so the only registration in flight is the one
// channel_park_prepare_locked appended — and that one carries a NONZERO park
// generation. The fix reads that generation before park_requeue_locked
// requeues the task (which clears park_key and park_prepared but not
// park_seq) and retires the entry after the owner lock is released, exactly
// as the post-commit abort below it already does. Defining the negative
// control zeroes the generation, which makes the retirement inert on THIS
// branch only, so the deterministic proof MUST observe the registration
// outliving the task that owned it.
#ifdef RV2_DEBT_201_NEGATIVE_CONTROL
#define RT_DEBT201_ABORT_SEQ(task, key) ((uint32_t)0)
#else
#define RT_DEBT201_ABORT_SEQ(task, key)                                                            \
    (((key).kind == WAKER_CHAN_SEND || (key).kind == WAKER_CHAN_RECV) ? (task)->park_seq           \
                                                                      : (uint32_t)0)
#endif

// RV2-DEBT-199 negative-control toggle. A far channel IS freed
// (rt_far_channel.c release_entry -> rt_channel_free), so resolving a channel
// waiter key by casting key.id back to rt_channel* and reading the object is a
// use-after-free from the moment the reclaim lands — and the reader is the
// ROUTING path itself (rt_waiter_route.c), reached by remove_waiter /
// remove_waiter_generation / add_waiter / wake_key_all with a key those callers
// merely carry. The fix stamps the channel's owner shard INTO the key at
// construction time, where the channel is provably alive because the operation
// building the key holds it, so routing reads the key and never the object.
// Defining the negative control restores the dereference, which the
// deterministic proof MUST observe as an ASan heap-use-after-free.
//
// IT MUST NOW BE DEFINED TOGETHER WITH RV2_CHANNEL_PIN_NEGATIVE_CONTROL below.
// A store entry on a channel key holds an internal pin, so while the entry
// exists the object is alive and the restored dereference reads live storage:
// this control alone no longer reproduces anything, and a control that
// reproduces nothing is a proof that passes without running. The pin is a
// SECOND defence against the same hazard, not a replacement for this fix -- a
// key a caller merely CARRIES is still a copy nothing counts, and the routing
// path is reached with exactly such a copy -- so the two controls compose: the
// pin control removes the hold, this one restores the dereference, and together
// they are the pre-fix world the ASan run must fail in.
#ifdef RV2_DEBT_199_NEGATIVE_CONTROL
#define RT_DEBT199_CHANNEL_OWNER_SHARD(key)                                                        \
    rt_channel_owner_shard_id((const rt_channel*)(uintptr_t)(key).id)
#else
#define RT_DEBT199_CHANNEL_OWNER_SHARD(key) ((key).owner_shard_id)
#endif

// RV2-DEBT-155 negative-control toggle, in two halves because the fix has two.
//
// A channel is reclaimed when nothing names it any more, and the count that
// says so is shared: a handle copied into another shard's frame is retired by
// a lane the creating one never sees. The fix makes both the retain and the
// release ATOMIC read-modify-writes, so no update is lost and exactly one
// releaser can observe the transition to zero. Defining the negative control
// restores the plain load-modify-store pair, which two lanes can interleave.
// ThreadSanitizer reports it as a data race at the instruction itself; without
// a sanitizer the same window surfaces two ways, and BOTH are worth naming
// because they look nothing alike -- a lost increment frees the object while a
// holder is still using it (a use-after-free, or a double free), and a lost
// decrement frees it never (the channel and every payload it holds are lost).
//
// The second half is the fail-closed quiescence assertion in
// rt_channel_free -- no handle, no pin. Defining the negative control makes it
// inert, so a reclaim that races a live holder proceeds into the drain and the
// use-after-free lands where a sanitizer can name it, instead of being refused
// with a sentence at the point of the mistake.
//
#ifdef RV2_DEBT_155_NEGATIVE_CONTROL
#define RT_DEBT155_HANDLE_ACQUIRE(refs) ((*(uint32_t*)(void*)(refs))++)
#define RT_DEBT155_HANDLE_RELEASE(refs) ((*(uint32_t*)(void*)(refs))--)
#define RT_DEBT155_STILL_NAMED(count) ((void)(count), (uint32_t)0)
#else
#define RT_DEBT155_HANDLE_ACQUIRE(refs) atomic_fetch_add_explicit((refs), 1, memory_order_relaxed)
#define RT_DEBT155_HANDLE_RELEASE(refs) atomic_fetch_sub_explicit((refs), 1, memory_order_acq_rel)
#define RT_DEBT155_STILL_NAMED(count) (count)
#endif

// The channel PIN's own negative control, separate from the handle count's
// above because it removes a different defence and each must be removable
// alone.
//
// A handle answers the language's question -- may a program still send on
// this? -- and a pin answers the runtime's: is a registered waiter, a select
// subscription, or a claimed operation still INSIDE the object? Only the pin
// can refuse a reclaim that no handle is left to refuse.
//
// Defining this makes a pin count for nothing, so the count never leaves zero
// and a reclaim proceeds under a live holder. Two other legs go inert with it,
// and both must: the registration leg of the quiescence check, which exists
// because a registration is a pin and would otherwise refuse the very reclaim
// this control is asking to let through; and the caller-holds-a-pin check on
// the claim/finish surface, which in a run where no pin is ever counted would
// fire on the first select instead of at the defect. Either would end the run
// at a panic rather than at the thing the control is trying to show. What is
// left is the pre-pin world -- a program that drops its last handle while a
// waiter is registered destroys the channel's buffered payloads there, a
// difference the census states as a number, and the key that registration still
// holds names freed storage.
//
// It is spelled as the ADDEND rather than as the whole operation because the
// admission and the count are one compare-and-swap on one word (see
// rt_channel_refcount.c): removing the exchange would remove the ordering the
// rest of the runtime reads, not the hold this control is asking to remove.
#ifdef RV2_CHANNEL_PIN_NEGATIVE_CONTROL
#define RT_CHANNEL_PIN_COUNTS 0u
#define RT_CHANNEL_REGISTERED(count) ((void)(count), (size_t)0)
#else
#define RT_CHANNEL_PIN_COUNTS 1u
#define RT_CHANNEL_REGISTERED(count) (count)
#endif

// RV2-DEBT-080 negative-control toggle. The fixed release destroys a blocking
// job's captures through their own descriptor: an initialized state cell is
// walked and then freed. Defining the negative control spends the cell first,
// which leaves the dispose with only the block to free -- the pre-fix shallow
// free of the state block by two integers, under which every capture the state
// still holds is abandoned. The deterministic proof MUST observe that loss.
#ifdef RV2_DEBT_080_NEGATIVE_CONTROL
#define RT_DEBT080_RELEASE_STATE(cell)                                                             \
    do {                                                                                           \
        (void)rt_value_cell_commit_move(cell);                                                     \
        rt_value_cell_dispose(cell);                                                               \
    } while (0)
#else
#define RT_DEBT080_RELEASE_STATE(cell) rt_value_cell_dispose(cell)
#endif

// RV2-DEBT-080 second negative-control toggle, for the OPPOSITE error. The
// worker spends the state cell immediately before the body runs, because from
// that moment the captures are the body's and a cancellation landing mid-body
// must not come back for them. Defining this control removes the claim, so the
// release walks a state the body has already consumed -- the double free an
// unconditional walk would be. The cancel-after-claim proof MUST observe it.
#ifdef RV2_DEBT_080_WALK_ALWAYS_NEGATIVE_CONTROL
#define RT_DEBT080_CLAIM_STATE(cell) ((void)(cell))
#else
#define RT_DEBT080_CLAIM_STATE(cell) ((void)rt_value_cell_commit_move(cell))
#endif

// RV2-DEBT-261 negative-control toggle. rt_scope_join_all answers two things
// -- has the set drained, and did fail-fast fire -- and scope_on_child_done
// decides both in one critical section: the cancelled child that trips the
// flag is retired from the count under the same lock. The fix reads both in
// the register-then-verify re-check as well, so a completion landing between
// the first snapshot and the verify cannot answer "drained" with the flag from
// before it. Defining the negative control drops the flag from the verify (the
// pre-fix shape), which the deterministic proof MUST observe as a @failfast
// scope resolving Success after its child was cancelled.
#ifdef RV2_DEBT_261_NEGATIVE_CONTROL
#define RT_DEBT261_VERIFY_FAILFAST_OUT(failfast) ((void)(failfast), (bool*)NULL)
#else
#define RT_DEBT261_VERIFY_FAILFAST_OUT(failfast) (failfast)
#endif

// RV2-DEBT-263 negative-control toggle, in two halves because the defect had
// two sides that could both believe they won.
//
// A task's answer is decided at the moment its completion seals the cancel
// gate, not by whoever carried the kind into mark_done: `cancel` through a live
// handle is task-global and, before committed success, must be observed by
// every awaited entitlement (23-storage-model-and-typed-carrier-abi.md). The
// fix makes each side move one word with one compare-and-swap out of OPEN, so
// exactly one of them wins.
//
// Defining the negative control restores the pre-fix shape on BOTH sides: the
// completion commits the kind it was handed however the seal went, and the
// cancel writes its flag and believes it landed however the CAS went. The
// deterministic proofs MUST then observe what the runtime used to do -- a task
// committing Success after a cancel it was told about, and a task committing
// Success while its canceller believes it cancelled it.
//
// Both toggles leave the CAS itself in place and change only what is BELIEVED
// about its result, so the negative control cannot pass by removing the
// ordering the proofs are built on.
#ifdef RV2_DEBT_263_NEGATIVE_CONTROL
#define RT_DEBT263_COMMIT_SEALED(sealed) ((void)(sealed), 1)
#define RT_DEBT263_CANCEL_LANDED(requested, task)                                                  \
    ((void)(requested),                                                                            \
     atomic_store_explicit(                                                                        \
         &(task)->cancelled, (uint8_t)RT_TASK_CANCEL_REQUESTED, memory_order_release),             \
     1)
#else
#define RT_DEBT263_COMMIT_SEALED(sealed) (sealed)
#define RT_DEBT263_CANCEL_LANDED(requested, task) ((void)(task), (requested))
#endif

// Inline-claim split negative-control toggle. Claiming a task for a poll is
// three stores -- enqueued cleared, status RUNNING, wake token consumed -- and
// the fix makes them run inside the same critical section that took the task
// off the queue, the way the worker turn's own claim already does
// (rt_worker_turn.c). Defining the negative control puts them back after the
// unlock, split at the exact instant that matters: enqueued is cleared first,
// so the task reads READY and not enqueued while it sits in no queue at all.
// The deterministic proof MUST then observe a wake queueing a second entry for
// a task already taken for this thread's poll.
//
// Both builds reach the sync point at the same place, so the negative control
// cannot pass by removing the window the proof is built on.
#ifdef RV2_INLINE_CLAIM_SPLIT_NEGATIVE_CONTROL
#define RT_INLINE_CLAIM_UNDER_LOCK(task) ((void)(task))
// The pre-fix shape: the claim runs after the lock is gone, and the store that
// clears enqueued lands before the window below. That is what leaves the task
// reading READY-and-not-enqueued while it sits in no queue at all. The rest of
// the claim follows the window; re-storing the cleared flag is a no-op, so the
// sequence a wake sees here is exactly the one the split used to produce.
#define RT_INLINE_CLAIM_SPLIT_FIRST(task) task_enqueued_store((task), 0)
#define RT_INLINE_CLAIM_SPLIT_REST(task) claim_task_off_queue(task)
#else
#define RT_INLINE_CLAIM_UNDER_LOCK(task) claim_task_off_queue(task)
#define RT_INLINE_CLAIM_SPLIT_FIRST(task) ((void)(task))
#define RT_INLINE_CLAIM_SPLIT_REST(task) ((void)(task))
#endif

// Scope provenance has no claim race: creation writes it once and publishes
// membership before runnable publication. Its negative control lives beside
// that protocol in rt_scope_membership.h; the legacy define remains an alias
// there so the old stand cannot accidentally exercise a different build.

// Canonical-pin negative-control toggle. A task's result is destroyed by the
// reclaim that runs when the LAST reference to a DONE task goes, and every
// asker inside a take is holding one of those references -- that reference is
// what pins the canonical value while a claimed clone reader duplicates out of
// it with no lock held. Shutdown does not change the rule: it stops new
// entitlements and lets claimed work finish before the canonical slot is
// dropped. Defining this control reads "shutdown drops the canonical result"
// literally, dropping it on the first release after shutdown however many
// askers still hold the task, so a reader that is out at that instant is
// duplicating out of a slot that has already been destroyed. The deterministic
// proof MUST observe the destruction landing while the claim is out.
#ifdef RV2_SHUTDOWN_UNPINNED_CANONICAL_NEGATIVE_CONTROL
#define RT_CANONICAL_UNPINNED(ex, refs) ((ex)->shutdown != 0 || (refs) == 1)
#else
#define RT_CANONICAL_UNPINNED(ex, refs) ((refs) == 1)
#endif

// Committed-result negative-control toggle. `cancel` through a live handle is
// task-global, and it is refused once the task's answer is committed: it does
// not revoke results that are already available to the other entitlements.
// Defining this control lets such a cancel empty the result slot instead, which
// is exactly the entitlement-local revocation the model forbids -- one handle
// keeps a value it was served while its siblings are told there is none. The
// deterministic proof MUST observe that revocation landing on a committed
// result. The default expands to nothing, so cancel_task's shape is unchanged.
#ifdef RV2_CANCEL_REVOKES_COMMITTED_RESULT_NEGATIVE_CONTROL
#define RT_CANCEL_AFTER_COMMITTED_RESULT(ex, task) rt_task_result_refuse((ex), (task))
#else
#define RT_CANCEL_AFTER_COMMITTED_RESULT(ex, task) ((void)(ex), (void)(task))
#endif

// Stale-generation negative-control toggle. A result capability names a slot by
// four integers, and the slot's own generation is the one that says WHICH
// occupant: an id can be reused and storage can be rebound, so a capability
// minted for one occupant must not be spendable on the next one in the same
// bytes. Defining this control drops the generation from the comparison,
// leaving the task id and "a value is there" to answer -- the state in which
// late work reaches into reused storage. The deterministic proof MUST observe
// the late holder being handed the occupant it never named.
#ifdef RV2_STALE_RESULT_GENERATION_NEGATIVE_CONTROL
#define RT_RESULT_GENERATION_MATCHES(cell, source) ((void)(cell), (void)(source), 1)
#else
#define RT_RESULT_GENERATION_MATCHES(cell, source)                                                 \
    (rt_value_cell_generation(cell) == (source)->result_generation)
#endif

#endif // RT_SYNC_POINT_H
