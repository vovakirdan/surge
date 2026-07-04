#ifndef SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#include "rt.h"
#include "rt_heap_accounting.h"
#include "rt_runtime_config.h"
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

typedef enum {
    SCHED_PARALLEL = 0,
    SCHED_SEEDED = 1,
} sched_mode;
typedef enum {
    RT_TRACE_SCHED_SRC_LOCAL = 0,
    RT_TRACE_SCHED_SRC_INJECT = 1,
    RT_TRACE_SCHED_SRC_STEAL = 2,
} rt_trace_sched_source;
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
    // until the queues drain (the Epic 7 baseline 8x1024 stall).
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
    rt_shard shards[RT_RUNTIME_MAX_SHARDS];
};

typedef struct rt_task {
    uint64_t id;
    int64_t poll_fn_id;
    void* state;
    uint64_t result_bits;
    uint8_t result_kind;
    atomic_u8 status;
    uint8_t kind;
    uint8_t resume_kind;
    uint8_t placement_class;
    uint8_t owner_shard_valid;
    uint32_t owner_shard_id;
    atomic_u8 cancelled;
    atomic_u8 enqueued;
    atomic_u8 wake_token;
    atomic_u8 polling;
    uint8_t checkpoint_polled;
    uint8_t sleep_armed;
    uint8_t park_prepared;
    uint8_t scope_registered;
    uint8_t cancel_pending;
    atomic_u32 handle_refs;
    uint64_t resume_bits;
    uint64_t sleep_delay;
    uint64_t sleep_deadline;
    uint64_t scope_id;
    uint64_t parent_scope_id;
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
} rt_task;

typedef struct {
    uint64_t id;
    uint64_t owner;
    uint8_t failfast;
    uint8_t failfast_triggered;
    uint64_t failfast_child;
    size_t active_children;
    uint64_t* children;
    size_t children_len;
    size_t children_cap;
} rt_scope;

// Task id -> task pointer table (D3): readers on any lane load the table
// pointer and a slot with acquire; growth allocates a copy and publishes it
// with release under the control lock. Retired generations are deliberately
// never freed - doubling bounds them to less than the live table's size -
// so a reader can never dereference a reallocated array.
typedef struct rt_task_table {
    size_t cap;
    _Atomic(rt_task*) slots[];
} rt_task_table;

struct rt_executor {
    uint64_t next_id;
    uint64_t next_scope_id;
    // Virtual clock (D7): relaxed atomic counter; ticks are fetch_add, idle
    // jumps go through rt_clock_advance_to (monotonic CAS).
    _Atomic uint64_t now_ms;
    rt_runtime* runtime;
    _Atomic(rt_task_table*) tasks_table;
    rt_scope** scopes;
    size_t scopes_cap;
    pthread_mutex_t lock;
    // compat_cv sleeps sync-channel compatibility waiters under the control
    // lock; worker sleep lives on each shard's worker_cv since Task 7.
    pthread_cond_t compat_cv;
    pthread_cond_t done_cv;
    pthread_t* workers;
    uint8_t initialized;
    uint8_t io_started;
    // Written on the control lane; read by shard-side wait predicates.
    _Atomic uint8_t shutdown;
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
    // Control-lane waiter store: scope keys only (D8). Everything else is
    // owner-resolved by rt_waiter_store_for_key.
    rt_waiter_store control_waiters;
    // Main-thread awaiters parked on done_cv (D10): completions broadcast
    // only when this is nonzero, so plain task exits skip the control cv.
    atomic_u32 done_waiters;
};

// Executor invariants:
// - ex->lock owns tasks/scopes, shard stores, scheduler queues/counters,
//   channel/blocking compatibility counters, net polling, timers, and shutdown.
// - task status is atomic for external observation; queue/waiter transitions
//   still happen under ex->lock.
// - waiter_store is FIFO-by-key. prepare_park may pre-register before
//   TASK_WAITING; wake_token closes wake-before-park races.
// - ready queues hold enqueued task ids. Workers pop local, inject, then steal;
//   non-worker threads inject globally.
// - running_count increments/decrements under ex->lock around user polls.
// - channel_blocking_compat tracks sync-channel worker parking; compensation
//   workers are a fallback for that path, not normal async parking.
// - The I/O thread is signaled on idle, net waiter registration, and shutdown.

typedef struct rt_channel rt_channel;

typedef struct {
    uint8_t kind;
    waker_key park_key;
    void* state;
    uint64_t value_bits;
} poll_outcome;

typedef struct rt_blocking_job {
    uint64_t task_id;
    uint64_t fn_id;
    void* state;
    uint64_t state_size;
    uint64_t state_align;
    uint64_t result_bits;
    atomic_u8 status;
    atomic_u8 cancel_requested;
    atomic_u32 refs;
    struct rt_blocking_job* next;
} rt_blocking_job;

void panic_msg(const char* msg);
int rt_async_debug_enabled(void);
void rt_async_debug_printf(const char* fmt, ...);
int rt_exec_trace_enabled(void);
void rt_exec_trace_init(void);
void rt_sched_trace_init(void);
int rt_trace_dump_requested(void);
void rt_trace_sched_record(rt_trace_sched_source source, uint64_t id);
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
// Lock-split counters (Epic 7 Task 12): steady-path serialization evidence.
void rt_trace_control_lock_acquired(void);
void rt_trace_cross_shard_wake(void);
void rt_trace_spurious_wake_absorbed(void);
void rt_trace_collect_wake_batch(void);
void rt_trace_owner_replaced(void);

// Per-site attribution of control-lane acquisitions on the task/scope
// lifecycle census paths (Epic 8 Task 5). Additive over control_lock_acquired:
// each census site tags its acquisition so Tasks 6-10 can measure the
// per-request control traffic each migration slice peels. Order matches the
// TRACE_EXEC dump fields. RT_CTRL_SITE_HANDLE covers the Task 7 handle slice
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

static inline uint8_t task_status_load(const rt_task* task) {
    return task == NULL ? TASK_DONE : atomic_load_explicit(&task->status, memory_order_acquire);
}

static inline void task_status_store(rt_task* task, uint8_t status) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->status, status, memory_order_release);
}

static inline uint8_t task_enqueued_load(const rt_task* task) {
    return task == NULL ? 0 : atomic_load_explicit(&task->enqueued, memory_order_acquire);
}

static inline void task_enqueued_store(rt_task* task, uint8_t value) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->enqueued, value, memory_order_release);
}

static inline uint8_t task_cancelled_load(const rt_task* task) {
    return task == NULL ? 1 : atomic_load_explicit(&task->cancelled, memory_order_acquire);
}

static inline void task_cancelled_store(rt_task* task, uint8_t value) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->cancelled, value, memory_order_release);
}

static inline uint8_t task_wake_token_exchange(rt_task* task, uint8_t value) {
    if (task == NULL) {
        return 0;
    }
    return atomic_exchange_explicit(&task->wake_token, value, memory_order_acq_rel);
}

static inline void task_polling_enter(rt_task* task) {
    if (task == NULL) {
        return;
    }
    if (atomic_exchange_explicit(&task->polling, 1, memory_order_acq_rel) != 0) {
        panic_msg("async: double poll");
    }
}

static inline void task_polling_exit(rt_task* task) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->polling, 0, memory_order_release);
}

extern void
__surge_poll_call(uint64_t id); // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
extern uint64_t __surge_blocking_call(uint64_t id, void* state);

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
rt_task_table* rt_task_table_snapshot(rt_executor* ex);
void ensure_scope_cap(rt_executor* ex, uint64_t id);
void ensure_child_cap(rt_task* task, size_t want);
void ensure_scope_child_cap(rt_scope* scope, size_t want);

uint8_t rt_channel_try_recv_status_locked(rt_executor* ex, void* channel, uint64_t* out_bits);
uint8_t rt_channel_try_recv_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                uint64_t* out_bits);
uint8_t rt_channel_try_send_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                uint64_t value_bits);
uint8_t rt_channel_try_send_status_locked(rt_executor* ex, void* channel, uint64_t value_bits);
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
int ready_take_current_local_tail(rt_executor* ex, uint64_t id);
int ready_pop(rt_executor* ex, uint64_t* out_id);
void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_net_task(rt_executor* ex, uint64_t id);
int channel_wake_force_inject_enabled(void);
void wake_key_all(rt_executor* ex, waker_key key);
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
void rt_sched_wake_signal_shard_n(rt_shard* shard, uint32_t tokens);
void rt_sched_wake_broadcast_all(rt_executor* ex);
int rt_sched_idle_sample_locked(rt_executor* ex);
void rt_sleep_store_init(rt_sleep_store* store);
rt_runtime_status rt_sleep_store_add(rt_sleep_store* store, uint64_t deadline, uint64_t task_id);
int rt_sleep_store_remove(rt_sleep_store* store, uint64_t task_id);
int rt_sleep_store_pop_due(rt_sleep_store* store, uint64_t now, uint64_t* out_task_id);
uint64_t rt_sleep_store_min(const rt_sleep_store* store);
void rt_sleep_store_destroy(rt_sleep_store* store);
uint64_t rt_clock_now(const rt_executor* ex);
uint64_t rt_clock_tick(rt_executor* ex);
int rt_clock_advance_to(rt_executor* ex, uint64_t target);
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
void scope_cancel_children_locked(rt_executor* ex, const rt_scope* scope);
void scope_child_done_locked(rt_executor* ex, rt_scope* scope, uint64_t child_id);
void scope_exit_locked(rt_executor* ex, rt_scope* scope);

void task_add_ref(rt_task* task);
void task_release(rt_executor* ex, rt_task* task);
void task_release_lane_aware(rt_executor* ex, rt_task* task);

void* rt_channel_new(uint64_t capacity);
bool rt_channel_send(void* channel, uint64_t value_bits);
bool rt_channel_send_yield(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv(void* channel, uint64_t* out_bits);
bool rt_channel_try_send(void* channel, uint64_t value_bits);
bool rt_channel_try_recv(void* channel, uint64_t* out_bits);
void rt_channel_close(void* channel);

int current_task_cancelled(rt_executor* ex);
void cancel_task(rt_executor* ex, uint64_t id);
void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind, uint64_t result_bits);
void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome);
rt_runtime_status rt_executor_request_shutdown(rt_executor* ex);
rt_runtime_status rt_executor_drain_shutdown_net_waiters(rt_executor* ex);

poll_outcome poll_task(rt_executor* ex, rt_task* task);
poll_outcome poll_blocking_task(rt_executor* ex, rt_task* task);
int rt_net_poll_wake_init(rt_shard* shard);
void rt_net_poll_wake_close(rt_net_poll_wake* wake);
void rt_net_poll_wake_drain(rt_shard* shard);
int rt_net_has_waiters_on_shard(const rt_executor* ex, uint32_t owner_shard_id);
int rt_net_begin_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id);
int rt_net_poll_waiters_owned_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms);
int poll_net_waiters_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms);
uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id);
uint64_t rt_net_wake_poll_all_shards(rt_executor* ex);
uint64_t
rt_net_wake_poll_for_task_wait_keys(rt_executor* ex, const rt_task* task, waker_key fallback_key);
void rt_net_trace_dump(const char* reason);
void rt_trace_sched_tier1_steal_denied(void);
void rt_trace_sched_connection_owner_placed(void);
void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id);
void rt_trace_drain_signal_dump(void);
int run_ready_one(rt_executor* ex);
int rt_run_ready_one_nowait_locked(rt_executor* ex);
void* rt_worker_main(void* arg);
void* rt_io_main(void* arg);
void rt_io_wait_slice(rt_executor* ex);
void rt_io_poll_nudge(rt_executor* ex);
int worker_next_ready(rt_worker_ctx* ctx, uint64_t* out_id);
int rt_next_sleep_deadline(const rt_executor* ex, uint64_t* out_deadline);
void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits);
int rt_wait_current_worker_wakeup(rt_executor* ex, rt_task* task);
rt_scheduler* current_worker_scheduler(const rt_executor* ex);
void maybe_start_compensation_worker_locked(rt_executor* ex);

#endif
