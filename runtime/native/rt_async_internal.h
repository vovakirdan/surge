#ifndef SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H

#include "rt.h"

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
    WAKER_NONE = 0,
    WAKER_JOIN = 1,
    WAKER_TIMER = 2,
    WAKER_CHAN_SEND = 3,
    WAKER_CHAN_RECV = 4,
    WAKER_NET_ACCEPT = 5,
    WAKER_NET_READ = 6,
    WAKER_NET_WRITE = 7,
    WAKER_SCOPE = 8,
    WAKER_BLOCKING = 9,
} waker_kind;

typedef enum {
    SCHED_PARALLEL = 0,
    SCHED_SEEDED = 1,
} sched_mode;

typedef struct {
    uint8_t kind;
    uint64_t id;
} waker_key;

typedef struct {
    waker_key key;
    uint64_t task_id;
} waiter;

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

typedef enum {
    RT_RUNTIME_STATUS_OK = 0,
    RT_RUNTIME_STATUS_INVALID_ARGUMENT = 1,
    RT_RUNTIME_STATUS_ALLOCATION_FAILED = 2,
} rt_runtime_status;

typedef struct rt_executor rt_executor;
typedef struct rt_runtime rt_runtime;
typedef struct rt_shard rt_shard;
typedef struct rt_worker_ctx rt_worker_ctx;

#define RT_RUNTIME_SHARD_COUNT 1U

typedef struct {
    rt_deque inject;
    rt_deque* local_queues;
    rt_worker_ctx* worker_ctxs;
    uint32_t worker_count;
    uint32_t running_count;
    uint8_t sched_mode;
    uint64_t sched_seed;
} rt_scheduler;

typedef struct {
    void* fds;
    size_t fds_cap;
    void* pfds;
    size_t pfds_cap;
} rt_net_poll_scratch;

struct rt_shard {
    rt_runtime* runtime;
    rt_executor* executor;
    rt_scheduler scheduler;
    rt_net_poll_scratch net_poll_scratch;
    uint32_t shard_id;
};

struct rt_runtime {
    size_t shard_count;
    rt_shard shards[RT_RUNTIME_SHARD_COUNT];
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
    waker_key park_key;
    waker_key* wait_keys;
    size_t wait_keys_len;
    size_t wait_keys_cap;
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

struct rt_executor {
    uint64_t next_id;
    uint64_t next_scope_id;
    uint64_t now_ms;
    rt_runtime* runtime;
    rt_task** tasks;
    size_t tasks_cap;
    rt_scope** scopes;
    size_t scopes_cap;
    waiter* waiters;
    size_t waiters_len;
    size_t waiters_cap;
    size_t net_waiters_len;
    pthread_mutex_t lock;
    pthread_cond_t ready_cv;
    pthread_cond_t io_cv;
    pthread_cond_t done_cv;
    pthread_t* workers;
    uint32_t channel_blocked_workers;
    uint8_t net_polling;
    uint32_t compensation_count;
    uint32_t compensation_high_water;
    uint8_t initialized;
    uint8_t io_started;
    uint8_t shutdown;
    pthread_mutex_t blocking_lock;
    pthread_cond_t blocking_cv;
    pthread_t* blocking_workers;
    uint32_t blocking_count;
    uint8_t blocking_started;
    uint8_t blocking_shutdown;
    atomic_u32 blocking_running;
    atomic_u32 blocking_submitted;
    atomic_u32 blocking_completed;
    atomic_u32 blocking_cancel_requested;
    struct rt_blocking_job* blocking_head;
    struct rt_blocking_job* blocking_tail;
};

// Executor invariants:
// - ex->lock owns tasks[], scopes[], waiters, net waiter/poll scratch state,
//   the single shard scheduler queues/counters, net_polling,
//   channel_blocked_workers, compensation_count/high-water, timer state, and
//   shutdown flags.
// - task status is atomic so external helpers can observe it, but transitions that
//   touch queues or waiters still happen under ex->lock.
// - waiters is a FIFO-by-key registration list. prepare_park may pre-register a
//   waiter before the task stores TASK_WAITING; wake_task uses wake_token to close
//   wake-before-park races.
// - ready queues hold task ids whose enqueued flag is set. Worker threads pop local
//   queues first, then inject, then steal; non-worker threads inject globally.
// - running_count counts tasks currently being polled. User tasks may poll without
//   ex->lock, but the increment/decrement around that poll is protected by ex->lock.
// - channel_blocked_workers counts executor workers parked inside sync channel
//   helpers after temporarily leaving running_count. Compensation workers are a
//   fallback for that path, not a normal async parking mechanism.
// - The I/O thread is signaled when the executor becomes idle, when net waiters are
//   registered, or when shutdown changes. Workers sleep on ready_cv only after they
//   fail to find local, injected, stealable, or immediately pollable net work.

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
void rt_trace_channel_task_blocking_send(void);
void rt_trace_channel_task_blocking_recv(void);
void rt_trace_channel_handoff_yield(void);

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

waker_key waker_none(void);
int waker_valid(waker_key key);
waker_key join_key(uint64_t id);
waker_key timer_key(uint64_t id);
waker_key scope_key(uint64_t id);
waker_key channel_send_key(const rt_channel* ch);
waker_key channel_recv_key(const rt_channel* ch);
waker_key net_accept_key(int fd);
waker_key net_read_key(int fd);
waker_key net_write_key(int fd);
waker_key blocking_key(uint64_t id);

rt_executor* ensure_exec(void);
rt_runtime_status rt_runtime_init_global_n1(rt_executor* ex);
rt_runtime* rt_executor_runtime(rt_executor* ex);
rt_shard* rt_runtime_shard0(rt_runtime* runtime);
size_t rt_runtime_shard_count(const rt_runtime* runtime);
rt_scheduler* rt_shard_scheduler(rt_shard* shard);
const rt_scheduler* rt_shard_scheduler_const(const rt_shard* shard);
rt_scheduler* rt_executor_scheduler(rt_executor* ex);
const rt_scheduler* rt_executor_scheduler_const(const rt_executor* ex);
rt_net_poll_scratch* rt_shard_net_poll_scratch(rt_shard* shard);
rt_net_poll_scratch* rt_executor_net_poll_scratch(rt_executor* ex);
rt_runtime_status rt_shard_scheduler_init(rt_shard* shard,
                                          uint32_t worker_count,
                                          uint8_t sched_mode_value,
                                          uint64_t sched_seed);
uint32_t rt_runtime_default_worker_count(void);
uint32_t rt_runtime_default_blocking_count(uint32_t workers);
uint64_t rt_current_task_id(void);
rt_task* rt_current_task(void);
void rt_set_current_task(rt_task* task);
void rt_lock(rt_executor* ex);
void rt_unlock(rt_executor* ex);
void rt_blocking_init(rt_executor* ex);
void rt_blocking_request_cancel(rt_executor* ex, rt_task* task);
rt_task* get_task(rt_executor* ex, uint64_t id);
rt_scope* get_scope(rt_executor* ex, uint64_t id);

void ensure_task_cap(rt_executor* ex, uint64_t id);
void ensure_scope_cap(rt_executor* ex, uint64_t id);
void ensure_waiter_cap(rt_executor* ex);
void ensure_child_cap(rt_task* task, size_t want);
void ensure_scope_child_cap(rt_scope* scope, size_t want);

void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void clear_wait_keys(rt_executor* ex, rt_task* task);
void add_wait_key(rt_executor* ex, rt_task* task, waker_key key);
void prepare_park(rt_executor* ex, rt_task* task, waker_key key, int already_added);
int pop_waiter(rt_executor* ex, waker_key key, uint64_t* out_id);
uint8_t rt_channel_try_recv_status_locked(rt_executor* ex, void* channel, uint64_t* out_bits);
uint8_t rt_channel_try_send_status_locked(rt_executor* ex, void* channel, uint64_t value_bits);
void clear_select_timers(rt_executor* ex, rt_task* task);
void ready_push(rt_executor* ex, uint64_t id);
int ready_take_current_local_tail(rt_executor* ex, uint64_t id);
int ready_pop(rt_executor* ex, uint64_t* out_id);
void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_channel_task(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_channel_task_no_signal(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_key_all(rt_executor* ex, waker_key key);
void park_current(rt_executor* ex, waker_key key);
void tick_virtual(rt_executor* ex);
int advance_time_to_next_timer(rt_executor* ex);
int next_ready(rt_executor* ex, uint64_t* out_id);

rt_task* task_from_handle(void* handle);
uint64_t task_id_from_handle(void* handle);

void task_add_child(rt_task* parent, uint64_t child_id);
void scope_add_child(rt_scope* scope, uint64_t child_id);
int scope_remove_child(rt_scope* scope, uint64_t child_id);
void scope_cancel_children_locked(rt_executor* ex, const rt_scope* scope);
void scope_child_done_locked(rt_executor* ex, rt_scope* scope, uint64_t child_id);
void scope_exit_locked(rt_executor* ex, rt_scope* scope);

void task_add_ref(rt_task* task);
void task_release(rt_executor* ex, rt_task* task);

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

poll_outcome poll_task(rt_executor* ex, rt_task* task);
poll_outcome poll_blocking_task(rt_executor* ex, rt_task* task);
int poll_net_waiters(rt_executor* ex, int timeout_ms);
void rt_net_wake_poll(void);
void rt_net_trace_dump(const char* reason);
void rt_trace_drain_signal_dump(void);
int run_ready_one(rt_executor* ex);
void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits);
int rt_wait_current_worker_wakeup(rt_executor* ex, rt_task* task);

#endif
