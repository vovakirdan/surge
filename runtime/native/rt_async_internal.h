#ifndef SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#include "rt.h"
#include "rt_async_trace.h"
#include "rt_heap_accounting.h"
#include "rt_park_pool.h"
#include "rt_placement.h"
#include "rt_runtime_config.h"
#include "rt_task_entitlement.h"
#include "rt_transport.h"
#include "rt_value_cell.h"
#include "rt_waiter.h"
#include <pthread.h>
#include <setjmp.h>
#include <stdatomic.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
// Async runtime internals shared across modules.
typedef enum {
    // READY tasks must be in exactly one ready queue or about to be queued.
    TASK_READY = 0,
    // RUNNING tasks are being polled by one worker and counted in running_count.
    TASK_RUNNING = 1,
    // WAITING tasks are parked behind at least one waker_key until wake_task runs.
    TASK_WAITING = 2,
    // DONE is terminal; tasks may remain in tasks[] until the last handle is released.
    TASK_DONE = 3,
} task_status;

typedef enum {
    TASK_KIND_USER = 0,
    TASK_KIND_CHECKPOINT = 1,
    TASK_KIND_SLEEP = 2,
    TASK_KIND_BLOCKING = 3,
} task_kind;
typedef enum { TASK_PLACEMENT_GENERIC = 0, TASK_PLACEMENT_CONNECTION = 1 } task_placement_class;
typedef enum {
    TASK_RESULT_NONE = 0,
    TASK_RESULT_SUCCESS = 1,
    TASK_RESULT_CANCELLED = 2,
} task_result_kind;
typedef enum {
    RESUME_NONE = 0,
    RESUME_CHAN_RECV_VALUE = 1,
    RESUME_CHAN_RECV_CLOSED = 2,
    RESUME_CHAN_SEND_ACK = 3,
    RESUME_CHAN_SEND_CLOSED = 4,
} resume_kind;
typedef enum {
    POLL_NONE = 0,
    POLL_DONE_SUCCESS = 1,
    POLL_DONE_CANCELLED = 2,
    POLL_YIELDED = 3,
    POLL_PARKED = 4,
} poll_kind;

// Select arm-kind ABI: the compiler emits these values into rt_select_poll
// calls; the remote proxy selector ships the channel subset (RECV/SEND)
// across the transport. Do not renumber.
typedef enum {
    SELECT_TASK = 0,
    SELECT_CHAN_RECV = 1,
    SELECT_CHAN_SEND = 2,
    SELECT_TIMEOUT = 3,
    SELECT_DEFAULT = 4,
} rt_select_arm_kind;

typedef enum {
    SCHED_PARALLEL = 0,
    SCHED_SEEDED = 1,
} sched_mode;
typedef enum {
    BLOCKING_JOB_PENDING = 0,
    BLOCKING_JOB_DONE = 1,
    BLOCKING_JOB_CANCELLED = 2,
} blocking_job_status;

typedef struct {
    uint64_t* buf;
    size_t cap;
    size_t head;
    size_t len;
} rt_deque;

typedef _Atomic uint8_t atomic_u8;
typedef _Atomic uint32_t atomic_u32;

typedef struct rt_executor rt_executor;
typedef struct rt_runtime rt_runtime;
typedef struct rt_shard rt_shard;
typedef struct rt_worker_ctx rt_worker_ctx;
#include "rt_fd_registry.h"

typedef struct {
    rt_deque inject;
    rt_deque* local_queues;
    rt_worker_ctx* worker_ctxs;
    uint32_t worker_count;
    uint32_t running_count;
    // Work this shard has taken OUT of one structure and not yet put into
    // another (shard-lock-guarded, like running_count).
    //
    // Several paths collect under the shard lock and republish outside it —
    // D5 collect-then-wake, which exists because waking takes the target's
    // owner lock and that can be this same mutex. For the length of that gap
    // a collected task is in no sleep store, no waiter store, no ready queue
    // and no running_count, so every structure an observer knows how to read
    // says the executor has nothing to do. It does; the work is in a local
    // array on somebody's stack.
    //
    // That mattered because idleness is not only a report here: it is the
    // predicate the virtual clock advances on, so a sample landing in the gap
    // jumps time past a deadline that was already due. Counting the in-flight
    // batch is what lets rt_sched_idle_sample_locked see work it cannot
    // otherwise reach.
    uint32_t publishing_count;
    // Wake tokens for the shard worker_cv (shard-lock-guarded): producers
    // bump before signaling, sleepers consume before waiting, so wakes that
    // race the control-to-shard sleep transition are never lost.
    uint32_t wake_pending;
    uint8_t sched_mode;
    uint64_t sched_seed;
} rt_scheduler;

struct rt_worker_ctx {
    rt_executor* ex;
    rt_shard* shard;
    rt_scheduler* scheduler;
    rt_heap_accounting_cell* heap_cell;
    uint32_t shard_id;
    uint32_t worker_id;
    uint32_t worker_index;
    uint64_t sched_rng;
    // Fairness tick: every 61st pop drains the inject queue first so
    // force-injected wakes (net readiness, yields) cannot starve behind a
    // constantly refilled local LIFO under sustained load.
    uint32_t pop_tick;
    // Net fairness tick: every 61st turn runs a zero-timeout net poll pass
    // even though ready work exists - a continuously busy shard would
    // otherwise never poll its own fds and their readiness would starve
    // until the queues drain (the baseline 8x1024 stall).
    uint32_t net_tick;
};

typedef struct {
    void* fds;
    size_t fds_cap;
    void* pfds;
    size_t pfds_cap;
} rt_net_poll_scratch;

typedef struct {
    int read_fd;
    int write_fd;
} rt_net_poll_wake;

typedef struct {
    uint64_t deadline;
    uint64_t task_id;
} rt_sleep_entry;

typedef struct {
    // Sorted by (deadline, task_id); mutated under the owning lane. The
    // atomic mirror lets tick paths peek other shards without their locks.
    rt_sleep_entry* entries;
    size_t len;
    size_t cap;
    _Atomic uint64_t min_deadline;
} rt_sleep_store;

typedef struct {
    // Atomic so control-free wake paths may read it; writes stay control-lane.
    atomic_u32 channel_blocked_workers;
    uint32_t compensation_count;
    uint32_t compensation_high_water;
} rt_channel_blocking_compat;

typedef struct {
    _Atomic uint64_t resolve_attempts;
    _Atomic uint64_t exact_shard_resolutions;
    _Atomic uint64_t distributed_resolutions;
    _Atomic uint64_t distributed_non_caller_resolutions;
    _Atomic uint64_t invalid_shard_resolutions;
    _Atomic uint64_t unsupported_resolutions;
} rt_placement_debug_counters;
struct rt_shard {
    rt_runtime* runtime;
    rt_executor* executor;
    // Shard lane (D1): this lock owns the shard's scheduler queues, waiter
    // store, net poll state, and the schedulable state of tasks it owns.
    // worker_cv sleeps workers waiting for ready work; poller_cv sleeps
    // threads waiting on net-poll arbitration or idle transitions.
    pthread_mutex_t lock;
    pthread_cond_t worker_cv;
    pthread_cond_t poller_cv;
    rt_scheduler scheduler;
    rt_heap_accounting heap_accounting;
    rt_net_poll_scratch net_poll_scratch;
    rt_net_poll_wake net_poll_wake;
    rt_transport_state transport;
    rt_fd_registry fd_registry;
    rt_channel_blocking_compat channel_blocking_compat;
    rt_waiter_store waiter_store;
    rt_sleep_store sleep_store;
    uint32_t shard_id;
    uint8_t net_polling;
    // Pending io nudges (peel B4): rt_io_poll_nudge bumps this under the
    // shard lock before broadcasting poller_cv; rt_io_wait_slice consumes it
    // under the same lock before sleeping, so a nudge that lands between the
    // io thread's control release and its cond wait is never lost.
    uint32_t poller_nudges;
};

struct rt_runtime {
    size_t shard_count;
    _Atomic uint64_t placement_rr_next;
    rt_placement_debug_counters placement_debug;
    rt_shard shards[RT_RUNTIME_MAX_SHARDS];
};

typedef struct rt_task {
    uint64_t id;
    uint64_t generation;
    int64_t poll_fn_id;
    void* state;
    // Who may still ask for the result, and how a second asker is served:
    // counts, decided under the owner shard lock, never the handle refcount.
    rt_task_entitlements entitlements;
    // The one canonical result this task owns, at the width its type asks for.
    // See rt_value_cell.h: the task outlives every handle that can ask for it,
    // a small result lives in the task's own bytes, and a wider one takes the
    // single block the box it replaces already cost.
    //
    // Local and far askers reach the SAME slot. A far await is not a second
    // representation: the transport is in-process, so a reply carries a
    // capability naming this slot and the awaiting side moves the value
    // straight out of it. Nothing is boxed to fit a word on the way.
    rt_value_cell result;
    // A suspension frame a cancellation left behind, and the descriptor that
    // reclaims it: the width, the alignment and the members' drop, resolved
    // where the frame is handed over rather than carried as a number nothing
    // in this struct can size. Set once, before any re-park bookkeeping can
    // touch it, so a frame deferred across one or more scope-drain re-parks is
    // still found here when mark_done finally runs. mark_done consumes the pair
    // exactly once and clears both; what a release then means is the frame's
    // own answer (rt_frame.h).
    //
    // The yield that finds its task already cancelled is what fills it in
    // practice: a cancelled RETURN has already given its own frame back through
    // the same release, so it names no type and leaves this empty.
    void* reclaim_frame;
    const rt_value_ops* reclaim_frame_ops;
    uint8_t result_kind;
    atomic_u8 status;
    uint8_t kind;
    uint8_t resume_kind;
    uint8_t placement_class;
    uint8_t owner_shard_valid;
    uint32_t owner_shard_id;
    atomic_u32 join_owner_shard_id;
    atomic_u8 cancelled;
    atomic_u8 enqueued;
    atomic_u8 wake_token;
    atomic_u8 polling;
    atomic_u8 remote_handle_state;
    atomic_u8 far_task_result_state;
    _Atomic(struct rt_far_task_lease*) far_task_result_lease;
    uint8_t checkpoint_polled;
    uint8_t sleep_armed;
    uint8_t park_prepared;
    uint8_t scope_registered;
    uint8_t cancel_pending;
    // Handle count in the low 31 bits, "this task has completed" in the top
    // one: one word, because whether a drop may free the task is one question.
    // The protocol, and why asking it as two reads was a double free, is in
    // rt_task_refs.h.
    atomic_u32 handle_refs;
    // Where a channel value delivered to this task is waiting.
    //
    // A capability token -- owner, index, generation -- and not the value: the
    // scheduler mailbox carries control only, and three integers is what that
    // means concretely. The storage the token names belongs to the CHANNEL,
    // because this task's poll function can leave by longjmp and a value it
    // owned across the park would be lost with nobody left to free it.
    rt_park_token resume_slot;
    uint64_t sleep_delay;
    uint64_t sleep_deadline;
    waker_key active_scope_key;
    // Write-once provenance: the scope active at creation, or WAKER_NONE.
    // Placement, scheduling, wake and completion never rewrite it.
    waker_key creation_scope_key;
    // Park generation for channel candidate/validate: bumped when a channel
    // park registers and when this task consumes a delivered channel resume,
    // so a popped entry from a superseded park validates false instead of
    // redelivering into a reused mailbox.
    uint32_t park_seq;
    waker_key park_key;
    waker_key* wait_keys;
    size_t wait_keys_len;
    size_t wait_keys_cap;
    uint8_t net_ready_accept_valid;
    int net_ready_accept_fd;
    uint32_t net_ready_accept_owner_shard;
    uint64_t timeout_task_id;
    uint64_t* select_timers;
    size_t select_timers_len;
    size_t select_timers_cap;
    uint64_t* children;
    size_t children_len;
    size_t children_cap;
    // Links this task into the lane's list of tasks waiting to be freed once
    // the last scheduler lock is released. Freeing a task destroys its result,
    // which runs generated code, so it may not happen under a lock -- and the
    // list threads through the tasks themselves, so keeping that rule costs no
    // allocation at all. NULL outside that list.
    struct rt_task* reclaim_next;
} rt_task;

static inline uint32_t rt_task_join_owner_shard_id_load(const rt_task* task) {
    if (task == NULL) {
        return 0;
    }
    return atomic_load_explicit(&task->join_owner_shard_id, memory_order_acquire);
}

static inline void rt_task_join_owner_shard_id_store(rt_task* task, uint32_t shard_id) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->join_owner_shard_id, shard_id, memory_order_release);
}

typedef struct rt_scope {
    uint64_t id;
    uint64_t owner;
    // Pinned scope owner shard (S5-Q8/Q10): set once at
    // rt_scope_enter from the entering task's owner shard and NEVER changed.
    // Every scope-object mutation and the scope_key waiter store both serialize
    // on THIS shard's lock, so the scope's serialization lock is stable for its
    // whole life even if the owner TASK is later re-placed by F2 placement
    // adoption (rt_task_poll_adopt_placement) - resolving the lock through the
    // mobile owner task would split the scope across two locks and race.
    uint32_t owner_shard_id;
    uint8_t failfast;
    uint8_t failfast_triggered;
    uint64_t failfast_child;
    size_t active_children;
    uint64_t* children;
    size_t children_len;
    size_t children_cap;
} rt_scope;

// Segmented task table (S5-Q1 realization B): a segment,
// once published, is never freed or moved, so a task pointer's address is
// stable for the task's whole lifetime and an owner-lane publish can never
// race a concurrent growth the way the old copy-on-grow table could (the
// spike's -DUNSAFE_PUBLISH negative control). Segment size and the fixed
// directory bound are compile-time constants; the directory itself never
// grows or moves, only individual segments are lazily allocated. Directory
// memory (RT_TASK_TABLE_MAX_SEGMENTS pointers, 512KB here) is the same
// monotonic, never-freed growth shape the old table already had (retired
// generations were never freed either) - not new debt, just redistributed.
#define RT_TASK_TABLE_SEGMENT_SHIFT 12
#define RT_TASK_TABLE_SEGMENT_SIZE (1u << RT_TASK_TABLE_SEGMENT_SHIFT)
#define RT_TASK_TABLE_MAX_SEGMENTS (1u << 16)

typedef struct rt_task_segment {
    _Atomic(rt_task*) slots[RT_TASK_TABLE_SEGMENT_SIZE];
} rt_task_segment;

typedef struct rt_task_table {
    _Atomic(rt_task_segment*) segments[RT_TASK_TABLE_MAX_SEGMENTS];
} rt_task_table;

// Segmented scope table (S5-Q7): the exact same never-moved-slot
// atomic-snapshot structure as rt_task_table (Global Rule 5 - reuse the proven
// pattern, not a new one). get_scope becomes a lock-free acquire load so scope
// object bookkeeping can run on the scope owner shard lane instead of control.
// Scope ids are monotonic and never reused (rt_scope_enter fetch_adds
// next_scope_id); a slot cleared on scope_exit is release-stored NULL and never
// reallocated, so a late scope_key wake/remove for a freed id resolves get_scope
// to NULL (routed to shard 0, draining nothing) - no generation needed, the
// same S9-Q7/rule-6 monotonic-never-reused-id argument as join/scope waiters.
#define RT_SCOPE_TABLE_SEGMENT_SHIFT 12
#define RT_SCOPE_TABLE_SEGMENT_SIZE (1u << RT_SCOPE_TABLE_SEGMENT_SHIFT)
#define RT_SCOPE_TABLE_MAX_SEGMENTS (1u << 16)

typedef struct rt_scope_segment {
    _Atomic(rt_scope*) slots[RT_SCOPE_TABLE_SEGMENT_SIZE];
} rt_scope_segment;

typedef struct rt_scope_table {
    _Atomic(rt_scope_segment*) segments[RT_SCOPE_TABLE_MAX_SEGMENTS];
} rt_scope_table;

struct rt_executor {
    // Atomic: id allocation is a lock-free fetch_add on the
    // owner-lane create path; control-held creators (checkpoint/sleep/
    // blocking submit) may still use `++` directly (an atomic RMW too, just
    // with the implicit seq_cst order), safely mixed with the relaxed
    // fetch_add used elsewhere.
    _Atomic uint64_t next_id;
    // Atomic: scope-id allocation is a lock-free fetch_add on
    // the owner-lane rt_scope_enter path, mirroring next_id.
    _Atomic uint64_t next_scope_id;
    // Virtual clock (D7): relaxed atomic counter; ticks are fetch_add, idle
    // jumps go through rt_clock_advance_to (monotonic CAS).
    _Atomic uint64_t now_ms;
    // Virtual milliseconds handed out that no wall-clock wait paid for: yield
    // ticks, and idle jumps taken while nothing outside the process could make
    // a task runnable. now_ms <= monotonic_ms + this is an invariant, so the
    // wall bound below can refuse to move the clock FORWARD but can never
    // strand a deadline that is already due.
    _Atomic uint64_t clock_free_ms;
    rt_runtime* runtime;
    // Embedded (not a swappable pointer): the segmented table's directory is
    // fixed-size and never reallocated, so there is nothing to atomically
    // swap at this level. See the rt_task_table definition above.
    rt_task_table tasks_table;
    // Segmented atomic-snapshot scope table: same never-moved-
    // slot, fixed-directory shape as tasks_table; get_scope acquire-loads it.
    rt_scope_table scopes_table;
    pthread_mutex_t lock;
    // compat_cv sleeps sync-channel compatibility waiters under the control
    // lock; worker sleep lives on each shard's worker_cv since .
    pthread_cond_t compat_cv;
    pthread_cond_t done_cv;
    pthread_t* workers;
    uint8_t initialized;
    uint8_t io_started;
    // Written on the control lane; read by shard-side wait predicates.
    _Atomic uint8_t shutdown;
    struct rt_remote_task_state* remote_tasks;
    struct rt_far_channel_state* far_channels;
    pthread_mutex_t blocking_lock;
    pthread_cond_t blocking_cv;
    pthread_t* blocking_workers;
    struct rt_blocking_worker_ctx* blocking_worker_ctxs;
    uint32_t blocking_count;
    uint8_t blocking_started;
    uint8_t blocking_shutdown;
    atomic_u32 blocking_running;
    atomic_u32 blocking_submitted;
    atomic_u32 blocking_completed;
    atomic_u32 blocking_cancel_requested;
    struct rt_blocking_job* blocking_head;
    struct rt_blocking_job* blocking_tail;
    // Control-lane waiter store. Scope keys remain here for compatibility;
    // (S5-Q10) moved scope keys to the scope owner shard store, so this now
    // only backs the rt_waiter_store_for_key default (unknown waker kind) and
    // the diagnostic waiter dump. Everything else is owner-resolved.
    rt_waiter_store control_waiters;
    // Main-thread awaiters parked on done_cv (D10): completions broadcast
    // only when this is nonzero, so plain task exits skip the control cv.
    atomic_u32 done_waiters;
};

// Executor lane invariants (post-Epic-7 lane split, post-Epic-8 lifecycle):
// there is no single executor lock; every mutation belongs to one lane:
// - Control lane (ex->lock): task/scope TABLE GROWTH only (segment alloc); the
//   external/main-thread await compatibility path (done_cv broadcast, gated on
//   done_waiters, and compat_cv for the sync-channel lane); the cross-owner
//   residuals that still keep a control fallback this epic (scope_on_child_done
//   when a child's owner shard != the scope's pinned owner shard, failfast
//   scope_cancel_children_controlled, and the cancel_task sibling walk);
//   checkpoint/sleep/blocking submit; and compensation-worker bookkeeping.
//   control_waiters now backs only the unknown-waker-kind default and the
//   diagnostic waiter dump.
// - Shard lane (rt_shard.lock): the owning shard's scheduler ready queues
//   (local + inject), running_count, wake_pending, waiter_store (join, net, and
//   channel keys plus the scope owner's scope_key all route here), sleep_store,
//   net poll state / fd registry, per-shard channel_blocking_compat, and the
//   steady-state task/scope lifecycle: slot publish/read, park/wake
//   (rt_task_park.c), and scope-object bookkeeping on the scope's pinned owner
//   shard.
// - Atomic, no lock held: task->status (acquire/release; the TASK_DONE release
//   store publishes result_kind/result_bits written before it),
//   enqueued/cancelled/wake_token/polling/handle_refs, next_id/next_scope_id/
//   remote_handle_state, now_ms/shutdown, the task and scope table slots (acquire-loaded by
//   get_task/get_scope), the sleep_store min_deadline mirror, done_waiters, and
//   channel_blocked_workers.
// Carried wake races: waiter_store is FIFO-by-key; prepare_park may pre-register
// before TASK_WAITING and the wake_token closes the wake-before-park window
// (D5). Workers pop local, then inject, then steal; non-worker threads inject
// globally. The I/O thread is signaled on idle, net waiter registration, and
// shutdown.

typedef struct rt_channel rt_channel;

typedef struct {
    uint8_t kind;
    waker_key park_key;
    void* state;
} poll_outcome;

typedef struct rt_blocking_job {
    uint64_t task_id;
    uint64_t fn_id;
    // The captures the submission packed, at their own type. The job owns the
    // block compiled code reserved for them and destroys it through the
    // descriptor, which is what a pointer and two integers could not do: two
    // numbers free storage but cannot destroy what is INSIDE it.
    //
    // The cell also carries the one fact the release path needs and nothing
    // else records: whether the body took the captures. Handing the state to
    // the body MOVES it, so a cancellation landing mid-body finds a spent cell
    // and frees only the block, while a job cancelled before the body ran finds
    // it initialized and walks the members first.
    rt_value_cell state;
    // What the blocking body answered, at its own type. The job owns it from
    // the moment the body returns until the awaiting task's poll moves it into
    // that task's result, and destroys it if nobody ever comes: the job
    // outlives the worker thread's frame and the task's poll alike, which is
    // why the value cannot live in either.
    rt_value_cell result;
    atomic_u8 status;
    atomic_u8 cancel_requested;
    atomic_u32 refs;
    struct rt_blocking_job* next;
} rt_blocking_job;
void panic_msg(const char* msg);
void fatal_oom_msg(const char* msg);
int rt_async_debug_enabled(void);
void rt_async_debug_printf(const char* fmt, ...);
int rt_exec_trace_enabled(void);
void rt_exec_trace_init(void);
int rt_trace_dump_requested(void);
void rt_trace_wake_called(void);
void rt_trace_wake_enqueued(void);
void rt_trace_wake_ignored_completed(void);
void rt_trace_park_attempt(void);
void rt_trace_park_committed(void);
void rt_trace_worker_sleep(void);
void rt_trace_worker_wake(void);
void rt_trace_channel_blocking_wait(void);
void rt_trace_channel_task_blocking_send(void);
void rt_trace_channel_task_blocking_recv(void);
void rt_trace_channel_handoff_yield(void);
void rt_trace_compensation_started(void);
void rt_trace_parked_with_work(void);
// Lock-split counters: steady-path serialization evidence.
void rt_trace_control_lock_acquired(void);
void rt_trace_cross_shard_wake(void);
void rt_trace_spurious_wake_absorbed(void);
void rt_trace_collect_wake_batch(void);
void rt_trace_owner_replaced(void);
// F2: counts join-consume placement adoptions, distinct from
// rt_trace_owner_replaced's aggregate over every owner replacement.
void rt_trace_placement_adoption(void);

// Per-site attribution of control-lane acquisitions on the task/scope
// lifecycle census paths. Additive over control_lock_acquired:
// each census site tags its acquisition so can measure the
// per-request control traffic each migration slice peels. Order matches the
// TRACE_EXEC dump fields. RT_CTRL_SITE_HANDLE covers the handle slice
// (wake/inline-child-poll/cancel/clone/release); checkpoint and rt_sleep stay
// in OTHER (spawn-shaped, negligible on the net bench).
typedef enum {
    RT_CTRL_SITE_OTHER = 0,
    RT_CTRL_SITE_CREATE,
    RT_CTRL_SITE_JOIN_POLL,
    RT_CTRL_SITE_COMPLETION,
    RT_CTRL_SITE_SCOPE,
    RT_CTRL_SITE_AWAIT_COMPAT,
    RT_CTRL_SITE_HANDLE,
    RT_CTRL_SITE_COUNT
} rt_ctrl_site;

void rt_trace_control_lock_site(rt_ctrl_site site);

// RT_CTRL_SITE_HANDLE sub-sites remain additive: WAKE is rt_task_wake's
// scope-adoption fallback, CANCEL is rt_task_cancel, and FREE is the
// last-reference release.
typedef enum {
    RT_CTRL_HANDLE_WAKE = 0,
    RT_CTRL_HANDLE_CANCEL,
    RT_CTRL_HANDLE_FREE,
    RT_CTRL_HANDLE_COUNT
} rt_ctrl_handle_site;

void rt_trace_control_lock_handle_site(rt_ctrl_handle_site site);
void rt_done_cv_broadcast_after_done(rt_executor* ex);

// The task state words and their inline helpers. A fragment of this header,
// included here and nowhere else, once rt_task and rt_executor are complete.
#include "rt_task_state.h"

extern void
__surge_poll_call(uint64_t id); // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
// Runs one blocking body and writes its result INTO `out_dst`, which the
// runtime sized from that body's own result type. It used to RETURN a machine
// word, which meant a result wider than one was boxed on the way out and
// adopted on the way back in.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
extern void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

extern rt_executor exec_state;
extern _Thread_local jmp_buf* poll_env;
extern _Thread_local int poll_active;
extern _Thread_local poll_outcome poll_result;
extern _Thread_local waker_key pending_key;
extern _Thread_local uint64_t tls_current_id;
extern _Thread_local rt_task* tls_current_task;
extern _Thread_local rt_worker_ctx* tls_worker_ctx;
extern _Thread_local int tls_worker_id;

rt_executor* ensure_exec(void);
rt_runtime_status rt_runtime_init_global(rt_executor* ex, size_t shard_count);
rt_runtime_status rt_runtime_init_shard_schedulers(rt_runtime* runtime,
                                                   uint32_t worker_count,
                                                   uint8_t sched_mode_value,
                                                   uint64_t sched_seed);
void rt_runtime_destroy_global(void);
rt_runtime* rt_executor_runtime(rt_executor* ex);
rt_shard* rt_runtime_shard(rt_runtime* runtime, size_t index);
const rt_shard* rt_runtime_shard_const(const rt_runtime* runtime, size_t index);
rt_shard* rt_runtime_shard0(rt_runtime* runtime);
size_t rt_runtime_shard_count(const rt_runtime* runtime);
uint64_t rt_runtime_total_worker_count(const rt_runtime* runtime);
rt_heap_accounting* rt_executor_heap_accounting(rt_executor* ex);
rt_scheduler* rt_shard_scheduler(rt_shard* shard);
const rt_scheduler* rt_shard_scheduler_const(const rt_shard* shard);
rt_scheduler* rt_executor_scheduler(rt_executor* ex);
const rt_scheduler* rt_executor_scheduler_const(const rt_executor* ex);
rt_scheduler* rt_task_scheduler(rt_executor* ex, const rt_task* task);
rt_net_poll_scratch* rt_shard_net_poll_scratch(rt_shard* shard);
rt_net_poll_scratch* rt_executor_net_poll_scratch_for_shard(rt_executor* ex, size_t shard_index);
rt_net_poll_scratch* rt_executor_net_poll_scratch(rt_executor* ex);
rt_channel_blocking_compat* rt_shard_channel_blocking_compat(rt_shard* shard);
const rt_channel_blocking_compat* rt_shard_channel_blocking_compat_const(const rt_shard* shard);
rt_channel_blocking_compat* rt_executor_channel_blocking_compat(rt_executor* ex);
const rt_channel_blocking_compat* rt_executor_channel_blocking_compat_const(const rt_executor* ex);
rt_waiter_store* rt_shard_waiter_store(rt_shard* shard);
const rt_waiter_store* rt_shard_waiter_store_const(const rt_shard* shard);
rt_waiter_store* rt_executor_waiter_store_for_shard(rt_executor* ex, size_t shard_index);
const rt_waiter_store* rt_executor_waiter_store_const_for_shard(const rt_executor* ex,
                                                                size_t shard_index);
rt_waiter_store* rt_executor_waiter_store(rt_executor* ex);
const rt_waiter_store* rt_executor_waiter_store_const(const rt_executor* ex);
rt_runtime_status rt_shard_scheduler_init(rt_shard* shard,
                                          uint32_t worker_count,
                                          uint8_t sched_mode_value,
                                          uint64_t sched_seed);
uint32_t rt_runtime_default_worker_count(void);
uint32_t rt_runtime_default_blocking_count(uint32_t workers);
uint64_t rt_current_task_id(void);
rt_task* rt_current_task(void);
void rt_set_current_task(rt_task* task);
void rt_control_lock(rt_executor* ex);
void rt_control_unlock(rt_executor* ex);
void rt_shard_lock(rt_shard* shard);
void rt_shard_unlock(rt_shard* shard);
int rt_lane_debug_enabled(void);
int rt_lane_holds_control(void);
// Runs work deferred out from under a scheduler lock RIGHT NOW, for a lane
// that is about to stop existing. The ordinary path is rt_control_unlock,
// which does it at the moment the lane becomes free.
void rt_lane_run_deferred_now(void);
int rt_lane_holds_any_shard(void);
int rt_lane_holds_shard(uint32_t shard_id);
rt_runtime_status rt_shard_sync_init(rt_shard* shard);
void rt_shard_sync_destroy(rt_shard* shard);
void rt_blocking_init(rt_executor* ex);
void rt_blocking_request_cancel(rt_executor* ex, rt_task* task);
rt_task* get_task(rt_executor* ex, uint64_t id);
rt_scope* get_scope(rt_executor* ex, uint64_t id);

int deque_push_tail(rt_deque* dq, uint64_t id, const char* overflow_msg, const char* alloc_msg);
int deque_push_head(rt_deque* dq, uint64_t id, const char* overflow_msg, const char* alloc_msg);
int deque_pop_head(rt_deque* dq, uint64_t* out_id);
int deque_pop_tail(rt_deque* dq, uint64_t* out_id);
void ensure_task_cap(rt_executor* ex, uint64_t id);
void rt_task_slot_store(rt_executor* ex, uint64_t id, rt_task* task);
// rt_task_table_snapshot: an acquire snapshot of the
// exclusive upper bound on ids ever allocated (ex->next_id), used by the two
// full-table scanners (rt_async_waiter.c, rt_async_trace.c) as their
// get_task(ex, i) iteration bound instead of walking a table struct
// directly - the segmented directory is internal to rt_async_state.c /
// rt_task_table.c.
uint64_t rt_task_table_snapshot(rt_executor* ex);
// rt_task_table_segment_missing: lock-free peek used by
// __task_create's steady-state path to decide whether the rare, control-lane
// segment-growth branch is needed at all.
int rt_task_table_segment_missing(rt_executor* ex, uint64_t id);
// Segmented scope table (rt_scope_table.c): mirror of the task
// table helpers. ensure_scope_cap allocates the id's segment under the control
// lock (rare growth); rt_scope_slot_store release-stores into a never-moved
// slot; rt_scope_table_segment_missing is the lock-free peek rt_scope_enter
// uses to skip control on the steady path.
void ensure_scope_cap(rt_executor* ex, uint64_t id);
void rt_scope_slot_store(rt_executor* ex, uint64_t id, rt_scope* scope);
int rt_scope_table_segment_missing(rt_executor* ex, uint64_t id);
void ensure_child_cap(rt_task* task, size_t want);
void ensure_scope_child_cap(rt_scope* scope, size_t want);

// The owner-locked channel cores are declared in rt_channel_lane.h, where the
// claim types they speak in are defined. They cannot be declared here: this
// header is what that one includes.
void rt_channel_release_payload(void* channel, void* storage);
void clear_select_timers(rt_executor* ex, rt_task* task);
void ready_push(rt_executor* ex, uint64_t id);
int ready_push_task_locked(const rt_executor* ex,
                           rt_shard* owner_shard,
                           rt_task* task,
                           int force_inject,
                           int front,
                           int signal_ready);
int wake_task_on_shard_locked(const rt_executor* ex,
                              rt_shard* owner_shard,
                              rt_task* task,
                              int force_inject,
                              int front,
                              int signal_ready,
                              waker_key* out_stale_key);
// Takes the task off the CURRENT worker's local deque tail and claims it for
// this thread's poll -- both under the owner shard lock, one observation --
// returning whether it was there to take. Claiming means what the worker turn
// means by it: unqueued, RUNNING, wake token consumed (rt_ready_queue.c).
int ready_claim_current_local_tail(rt_executor* ex, uint64_t id);
// Extracted to rt_ready_queue.c; external because
// apply_poll_outcome (rt_task_complete.c) re-pushes yielded tasks across the
// module boundary.
int ready_push_yielded_task(rt_executor* ex, uint64_t id);
int ready_pop(rt_executor* ex, uint64_t* out_id);
void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_net_task(rt_executor* ex, uint64_t id);
int channel_wake_force_inject_enabled(void);
void wake_key_all(rt_executor* ex, waker_key key);
// Extracted to rt_task_park.c; external because mark_done
// (rt_async_state.c) drains join waiters across the module boundary.
void wake_key_all_with_policy(rt_executor* ex, waker_key key, int front);
void park_current(rt_executor* ex, waker_key key);
void tick_virtual(rt_executor* ex);
int advance_time_to_next_timer(rt_executor* ex);
int next_ready(rt_executor* ex, uint64_t* out_id);

rt_task* task_from_handle(void* handle);
rt_task* rt_spawn_sleep_task_locked(rt_executor* ex, uint64_t delay);
uint64_t task_id_from_handle(void* handle);
void rt_task_set_placement(rt_task* task, uint32_t shard_id, uint8_t placement_class);
void rt_task_replace_owner(rt_executor* ex,
                           rt_task* task,
                           uint32_t shard_id,
                           uint8_t placement_class);
void rt_task_inherit_placement(rt_task* task, const rt_task* parent);
void rt_task_assign_spawn_owner(rt_task* task);
rt_shard* rt_task_owner_shard(rt_executor* ex, const rt_task* task);
uint32_t rt_task_owner_shard_id(rt_executor* ex, const rt_task* task);
void rt_sched_wake_signal_shard_n(rt_shard* shard, uint32_t tokens);
void rt_sched_wake_broadcast_all(rt_executor* ex);
int rt_sched_idle_sample_locked(rt_executor* ex);
// Collect-then-wake bookkeeping. `begin` is called with the shard lock HELD,
// in the same critical section that removed the batch; `end` takes the lock
// itself, after the batch has been republished. See rt_scheduler.publishing_count.
void rt_sched_publishing_begin_locked(rt_shard* shard, size_t count);
void rt_sched_publishing_end(rt_shard* shard, size_t count);
void rt_sleep_store_init(rt_sleep_store* store);
rt_runtime_status rt_sleep_store_add(rt_sleep_store* store, uint64_t deadline, uint64_t task_id);
int rt_sleep_store_remove(rt_sleep_store* store, uint64_t task_id);
int rt_sleep_store_pop_due(rt_sleep_store* store, uint64_t now, uint64_t* out_task_id);
uint64_t rt_sleep_store_min(const rt_sleep_store* store);
void rt_sleep_store_destroy(rt_sleep_store* store);
uint64_t rt_clock_now(const rt_executor* ex);
uint64_t rt_clock_tick(rt_executor* ex);
int rt_clock_advance_to(rt_executor* ex, uint64_t target);
uint64_t rt_clock_deadline_base(rt_executor* ex);
int rt_clock_advance_to_next_deadline(rt_executor* ex);
size_t rt_sleep_fire_due_on_shard(rt_executor* ex, rt_shard* shard, uint64_t now);
int rt_task_can_steal_from_shard(const rt_task* task, uint32_t shard_id);
int rt_task_can_steal_from_shard_or_trace_denied(const rt_task* task, uint32_t shard_id);
void rt_debug_assert_no_parked_with_work(rt_executor* ex, uint32_t shard_id);
int rt_debug_validate_worker_ctx(rt_executor* ex,
                                 uint32_t shard_id,
                                 uint32_t worker_id,
                                 uint32_t worker_index);
uint32_t rt_debug_current_worker_shard_id(void);

void task_add_child(rt_task* parent, uint64_t child_id);
void scope_add_child(rt_scope* scope, uint64_t child_id);
int scope_remove_child(rt_scope* scope, uint64_t child_id);
// Scope owner lane (rt_async_scope.c): a scope key carries its PINNED owner
// shard, stable for the scope's life. Callers resolve the slot only while that
// lock is held; cancellation snapshots there, then walks under control.
#include "rt_scope_teardown.h"

void task_add_ref(rt_task* task);
void task_release(rt_executor* ex, rt_task* task);
void task_release_lane_aware(rt_executor* ex, rt_task* task);

void* rt_channel_new(uint64_t capacity, const rt_value_ops* ops, uint64_t element_type_id);
// Turns an element TYPE ID back into its descriptor. The far create path is
// the caller this exists for: a payload type crosses the boundary as a number.
const rt_value_ops* rt_channel_element_ops_for(uint64_t element_type_id);
// The descriptor for a channel of opaque machine words: what a far channel
// holds today, and what a C stand uses when no compiled code supplies one.
const rt_value_ops* rt_channel_opaque_word_ops(void);
bool rt_channel_send(void* channel, void* src);
bool rt_channel_send_yield(void* channel, void* src);
uint8_t rt_channel_recv(void* channel, void* dst);
bool rt_channel_try_send(void* channel, void* src);
bool rt_channel_try_recv(void* channel, void* dst);
void rt_channel_close(void* channel);
void rt_channel_free(void* channel);
void rt_channel_free_when_unlocked(void* channel);
void rt_channel_reclaim_drain(void);
// Frees the tasks whose reclamation had to wait for this lane to hold no
// scheduler lock, because freeing one destroys its result.
void rt_task_reclaim_drain(void);
// Empties the result slot of a completion that refused the value its body
// produced, destroying it once this lane holds no scheduler lock
// (RV2-DEBT-263).
void rt_task_result_refuse(rt_executor* ex, rt_task* task);

int current_task_cancelled(rt_executor* ex);
void cancel_task(rt_executor* ex, uint64_t id);
// Completes a task. The result value, if there is one, is already in the task's
// own slot: rt_async_return moved it there from inside the task's own poll,
// which is the one place it can run the element's move with no lock held.
void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind);
void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome);
rt_runtime_status rt_executor_request_shutdown(rt_executor* ex);
rt_runtime_status rt_executor_drain_shutdown_net_waiters(rt_executor* ex);

poll_outcome poll_task(rt_executor* ex, rt_task* task);
poll_outcome poll_blocking_task(rt_executor* ex, rt_task* task);
void rt_net_poll_wake_close(rt_net_poll_wake* wake);
uint64_t
rt_net_wake_poll_for_task_wait_keys(rt_executor* ex, const rt_task* task, waker_key fallback_key);
#include "rt_async_net_poll.h"
int run_ready_one(rt_executor* ex);
int rt_run_ready_one_nowait_locked(rt_executor* ex);
void* rt_worker_main(void* arg);
void* rt_io_main(void* arg);
void rt_io_wait_slice(rt_executor* ex);
void rt_io_poll_nudge(rt_executor* ex);
int worker_next_ready(rt_worker_ctx* ctx, uint64_t* out_id);
int rt_next_sleep_deadline(const rt_executor* ex, uint64_t* out_deadline);
void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind);
int rt_wait_current_worker_wakeup(rt_executor* ex, rt_task* task);
rt_scheduler* current_worker_scheduler(const rt_executor* ex);
void maybe_start_compensation_worker_locked(rt_executor* ex);

#endif
