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
    TASK_READY = 0,
    TASK_RUNNING = 1,
    TASK_WAITING = 2,
    TASK_DONE = 3,
} task_status;

typedef enum {
    TASK_KIND_USER = 0,
    TASK_KIND_CHECKPOINT = 1,
    TASK_KIND_SLEEP = 2,
    TASK_KIND_NET_ACCEPT = 3,
    TASK_KIND_NET_READ = 4,
    TASK_KIND_NET_WRITE = 5,
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

typedef struct {
    uint64_t* buf;
    size_t cap;
    size_t head;
    size_t len;
} rt_deque;

typedef _Atomic uint8_t atomic_u8;
typedef _Atomic uint32_t atomic_u32;

typedef struct rt_worker_ctx rt_worker_ctx;

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
    int net_fd;
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

typedef struct {
    uint64_t next_id;
    uint64_t next_scope_id;
    uint64_t now_ms;
    rt_task** tasks;
    size_t tasks_cap;
    rt_deque inject;
    rt_deque* local_queues;
    rt_scope** scopes;
    size_t scopes_cap;
    waiter* waiters;
    size_t waiters_len;
    size_t waiters_cap;
    pthread_mutex_t lock;
    pthread_cond_t ready_cv;
    pthread_cond_t io_cv;
    pthread_cond_t done_cv;
    pthread_t* workers;
    rt_worker_ctx* worker_ctxs;
    uint32_t worker_count;
    uint32_t running_count;
    uint8_t sched_mode;
    uint8_t initialized;
    uint8_t io_started;
    uint8_t shutdown;
    uint64_t sched_seed;
} rt_executor;

typedef struct rt_channel rt_channel;

typedef struct {
    uint8_t kind;
    waker_key park_key;
    void* state;
    uint64_t value_bits;
} poll_outcome;

void panic_msg(const char* msg);

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

extern rt_executor exec_state;
extern _Thread_local jmp_buf poll_env;
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
waker_key channel_send_key(rt_channel* ch);
waker_key channel_recv_key(rt_channel* ch);
waker_key net_accept_key(int fd);
waker_key net_read_key(int fd);
waker_key net_write_key(int fd);

rt_executor* ensure_exec(void);
uint64_t rt_current_task_id(void);
rt_task* rt_current_task(void);
void rt_set_current_task(rt_task* task);
void rt_lock(rt_executor* ex);
void rt_unlock(rt_executor* ex);
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
int ready_pop(rt_executor* ex, uint64_t* out_id);
void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag);
void wake_key_all(rt_executor* ex, waker_key key);
void park_current(rt_executor* ex, waker_key key);
void tick_virtual(rt_executor* ex);
int advance_time_to_next_timer(rt_executor* ex);
int next_ready(rt_executor* ex, uint64_t* out_id);

rt_task* task_from_handle(void* handle);
uint64_t task_id_from_handle(void* handle);

void task_add_child(rt_task* parent, uint64_t child_id);
void scope_add_child(rt_scope* scope, uint64_t child_id);
void scope_cancel_children_locked(rt_executor* ex, const rt_scope* scope);
void scope_child_done_locked(rt_executor* ex, rt_scope* scope);
void scope_exit_locked(rt_executor* ex, rt_scope* scope);

void task_add_ref(rt_task* task);
void task_release(rt_executor* ex, rt_task* task);

void* rt_channel_new(uint64_t capacity);
bool rt_channel_send(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv(void* channel, uint64_t* out_bits);
bool rt_channel_try_send(void* channel, uint64_t value_bits);
bool rt_channel_try_recv(void* channel, uint64_t* out_bits);
void rt_channel_close(void* channel);

int current_task_cancelled(rt_executor* ex);
void cancel_task(rt_executor* ex, uint64_t id);
void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind, uint64_t result_bits);

poll_outcome poll_task(rt_executor* ex, rt_task* task);
poll_outcome poll_net_task(const rt_executor* ex, const rt_task* task);
int poll_net_waiters(rt_executor* ex, int timeout_ms);
int run_ready_one(rt_executor* ex);
void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits);

#endif
