#ifndef SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_ASYNC_INTERNAL_H

#include "rt.h"

#include <setjmp.h>
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
} waker_kind;

typedef struct {
    uint8_t kind;
    uint64_t id;
} waker_key;

typedef struct {
    waker_key key;
    uint64_t task_id;
} waiter;

typedef struct rt_task {
    uint64_t id;
    int64_t poll_fn_id;
    void* state;
    uint64_t result_bits;
    uint8_t result_kind;
    uint8_t status;
    uint8_t kind;
    uint8_t cancelled;
    uint8_t resume_kind;
    uint8_t enqueued;
    uint8_t checkpoint_polled;
    uint8_t sleep_armed;
    uint32_t handle_refs;
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
    uint64_t* children;
    size_t children_len;
    size_t children_cap;
} rt_task;

typedef struct {
    uint64_t id;
    uint64_t owner;
    uint8_t failfast;
    uint8_t failfast_triggered;
    uint64_t* children;
    size_t children_len;
    size_t children_cap;
} rt_scope;

typedef struct {
    uint64_t next_id;
    uint64_t next_scope_id;
    uint64_t current;
    uint64_t now_ms;
    rt_task** tasks;
    size_t tasks_cap;
    uint64_t* ready;
    size_t ready_len;
    size_t ready_head;
    size_t ready_cap;
    rt_scope** scopes;
    size_t scopes_cap;
    waiter* waiters;
    size_t waiters_len;
    size_t waiters_cap;
} rt_executor;

typedef struct rt_channel rt_channel;

typedef struct {
    uint8_t kind;
    waker_key park_key;
    void* state;
    uint64_t value_bits;
} poll_outcome;

extern void
__surge_poll_call(uint64_t id); // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)

extern rt_executor exec_state;
extern jmp_buf poll_env;
extern int poll_active;
extern poll_outcome poll_result;
extern waker_key pending_key;

void panic_msg(const char* msg);

waker_key waker_none(void);
int waker_valid(waker_key key);
waker_key join_key(uint64_t id);
waker_key timer_key(uint64_t id);
waker_key channel_send_key(rt_channel* ch);
waker_key channel_recv_key(rt_channel* ch);

rt_executor* ensure_exec(void);
rt_task* get_task(rt_executor* ex, uint64_t id);
rt_scope* get_scope(rt_executor* ex, uint64_t id);

void ensure_task_cap(rt_executor* ex, uint64_t id);
void ensure_scope_cap(rt_executor* ex, uint64_t id);
void ensure_ready_cap(rt_executor* ex);
void ensure_waiter_cap(rt_executor* ex);
void ensure_child_cap(rt_task* task, size_t want);
void ensure_scope_child_cap(rt_scope* scope, size_t want);

void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void clear_wait_keys(rt_executor* ex, rt_task* task);
void add_wait_key(rt_executor* ex, rt_task* task, waker_key key);
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

poll_outcome poll_task(const rt_executor* ex, rt_task* task);
int run_ready_one(rt_executor* ex);
void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits);

#endif
