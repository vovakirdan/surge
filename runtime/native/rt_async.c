#include "rt.h"

#include <setjmp.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

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
    uint8_t enqueued;
    uint8_t checkpoint_polled;
    uint8_t sleep_armed;
    uint64_t sleep_delay;
    uint64_t sleep_deadline;
    uint64_t scope_id;
    uint64_t parent_scope_id;
    waker_key park_key;
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

typedef struct {
    uint8_t kind;
    waker_key park_key;
    void* state;
    uint64_t value_bits;
} poll_outcome;

extern void __surge_poll_call(uint64_t id);

static rt_executor exec_state;
static jmp_buf poll_env;
static int poll_active = 0;
static poll_outcome poll_result;
static waker_key pending_key;

static void panic_msg(const char* msg) {
    if (msg == NULL) {
        return;
    }
    rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
}

static waker_key waker_none(void) {
    waker_key key = {WAKER_NONE, 0};
    return key;
}

static int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

static waker_key join_key(uint64_t id) {
    waker_key key = {WAKER_JOIN, id};
    return key;
}

static waker_key timer_key(uint64_t id) {
    waker_key key = {WAKER_TIMER, id};
    return key;
}

static rt_executor* ensure_exec(void) {
    rt_executor* ex = &exec_state;
    if (ex->next_id == 0) {
        ex->next_id = 1;
    }
    if (ex->next_scope_id == 0) {
        ex->next_scope_id = 1;
    }
    return ex;
}

static rt_task* get_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0 || id >= ex->tasks_cap) {
        return NULL;
    }
    return ex->tasks[id];
}

static rt_scope* get_scope(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0 || id >= ex->scopes_cap) {
        return NULL;
    }
    return ex->scopes[id];
}

static void ensure_task_cap(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return;
    }
    if (id < ex->tasks_cap) {
        return;
    }
    size_t next_cap = ex->tasks_cap == 0 ? 8 : ex->tasks_cap;
    while (next_cap <= id) {
        next_cap *= 2;
    }
    size_t old_size = ex->tasks_cap * sizeof(rt_task*);
    size_t new_size = next_cap * sizeof(rt_task*);
    rt_task** next = (rt_task**)rt_realloc((uint8_t*)ex->tasks, (uint64_t)old_size, (uint64_t)new_size, sizeof(void*));
    if (next == NULL) {
        panic_msg("async: task allocation failed");
        return;
    }
    if (next_cap > ex->tasks_cap) {
        memset(next + ex->tasks_cap, 0, (next_cap - ex->tasks_cap) * sizeof(rt_task*));
    }
    ex->tasks = next;
    ex->tasks_cap = next_cap;
}

static void ensure_scope_cap(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return;
    }
    if (id < ex->scopes_cap) {
        return;
    }
    size_t next_cap = ex->scopes_cap == 0 ? 8 : ex->scopes_cap;
    while (next_cap <= id) {
        next_cap *= 2;
    }
    size_t old_size = ex->scopes_cap * sizeof(rt_scope*);
    size_t new_size = next_cap * sizeof(rt_scope*);
    rt_scope** next = (rt_scope**)rt_realloc((uint8_t*)ex->scopes, (uint64_t)old_size, (uint64_t)new_size, sizeof(void*));
    if (next == NULL) {
        panic_msg("async: scope allocation failed");
        return;
    }
    if (next_cap > ex->scopes_cap) {
        memset(next + ex->scopes_cap, 0, (next_cap - ex->scopes_cap) * sizeof(rt_scope*));
    }
    ex->scopes = next;
    ex->scopes_cap = next_cap;
}

static void ensure_ready_cap(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    if (ex->ready_len < ex->ready_cap) {
        return;
    }
    size_t next_cap = ex->ready_cap == 0 ? 16 : ex->ready_cap * 2;
    size_t old_size = ex->ready_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)ex->ready, (uint64_t)old_size, (uint64_t)new_size, sizeof(uint64_t));
    if (next == NULL) {
        panic_msg("async: ready queue allocation failed");
        return;
    }
    ex->ready = next;
    ex->ready_cap = next_cap;
}

static void ensure_waiter_cap(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    if (ex->waiters_len < ex->waiters_cap) {
        return;
    }
    size_t next_cap = ex->waiters_cap == 0 ? 16 : ex->waiters_cap * 2;
    size_t old_size = ex->waiters_cap * sizeof(waiter);
    size_t new_size = next_cap * sizeof(waiter);
    waiter* next = (waiter*)rt_realloc((uint8_t*)ex->waiters, (uint64_t)old_size, (uint64_t)new_size, sizeof(void*));
    if (next == NULL) {
        panic_msg("async: waiter allocation failed");
        return;
    }
    ex->waiters = next;
    ex->waiters_cap = next_cap;
}

static void ensure_child_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->children_cap >= want) {
        return;
    }
    size_t next_cap = task->children_cap == 0 ? 4 : task->children_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->children_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)task->children, (uint64_t)old_size, (uint64_t)new_size, sizeof(uint64_t));
    if (next == NULL) {
        panic_msg("async: child allocation failed");
        return;
    }
    task->children = next;
    task->children_cap = next_cap;
}

static void ensure_scope_child_cap(rt_scope* scope, size_t want) {
    if (scope == NULL) {
        return;
    }
    if (scope->children_cap >= want) {
        return;
    }
    size_t next_cap = scope->children_cap == 0 ? 4 : scope->children_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = scope->children_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)scope->children, (uint64_t)old_size, (uint64_t)new_size, sizeof(uint64_t));
    if (next == NULL) {
        panic_msg("async: scope child allocation failed");
        return;
    }
    scope->children = next;
    scope->children_cap = next_cap;
}

static void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    if (ex == NULL || ex->waiters_len == 0) {
        return;
    }
    size_t out = 0;
    for (size_t i = 0; i < ex->waiters_len; i++) {
        waiter w = ex->waiters[i];
        if (w.task_id == task_id && w.key.kind == key.kind && w.key.id == key.id) {
            continue;
        }
        ex->waiters[out++] = w;
    }
    ex->waiters_len = out;
}

static void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    ensure_waiter_cap(ex);
    ex->waiters[ex->waiters_len++] = (waiter){key, task_id};
}

static void ready_push(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task->status == TASK_DONE) {
        return;
    }
    if (task->enqueued) {
        return;
    }
    ensure_ready_cap(ex);
    ex->ready[ex->ready_len++] = id;
    task->enqueued = 1;
    task->status = TASK_READY;
}

static int ready_pop(rt_executor* ex, uint64_t* out_id) {
    if (ex == NULL) {
        return 0;
    }
    while (ex->ready_head < ex->ready_len) {
        uint64_t id = ex->ready[ex->ready_head++];
        rt_task* task = get_task(ex, id);
        if (task == NULL || task->status == TASK_DONE) {
            continue;
        }
        task->enqueued = 0;
        if (out_id != NULL) {
            *out_id = id;
        }
        if (ex->ready_head > 0 && ex->ready_head == ex->ready_len) {
            ex->ready_head = 0;
            ex->ready_len = 0;
        }
        return 1;
    }
    if (ex->ready_head > 0 && ex->ready_len > ex->ready_head) {
        size_t remaining = ex->ready_len - ex->ready_head;
        memmove(ex->ready, ex->ready + ex->ready_head, remaining * sizeof(uint64_t));
        ex->ready_len = remaining;
        ex->ready_head = 0;
    }
    return 0;
}

static void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag) {
    if (ex == NULL) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task->status == TASK_DONE) {
        return;
    }
    if (remove_waiter_flag && waker_valid(task->park_key)) {
        remove_waiter(ex, task->park_key, id);
        task->park_key = waker_none();
    }
    ready_push(ex, id);
}

static void wake_key_all(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    size_t out = 0;
    for (size_t i = 0; i < ex->waiters_len; i++) {
        waiter w = ex->waiters[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            wake_task(ex, w.task_id, 0);
            continue;
        }
        ex->waiters[out++] = w;
    }
    ex->waiters_len = out;
}

static void park_current(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key) || ex->current == 0) {
        return;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL || task->status == TASK_DONE) {
        return;
    }
    task->status = TASK_WAITING;
    task->park_key = key;
    add_waiter(ex, key, task->id);
}

static void tick_virtual(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    ex->now_ms++;
    if (ex->tasks_cap == 0) {
        return;
    }
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP || task->status != TASK_WAITING || !task->sleep_armed) {
            continue;
        }
        if (task->sleep_deadline <= ex->now_ms) {
            wake_task(ex, task->id, 1);
        }
    }
}

static int advance_time_to_next_timer(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t next_deadline = UINT64_MAX;
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP || task->status != TASK_WAITING || !task->sleep_armed) {
            continue;
        }
        if (task->sleep_deadline < next_deadline) {
            next_deadline = task->sleep_deadline;
        }
    }
    if (next_deadline == UINT64_MAX) {
        return 0;
    }
    ex->now_ms = next_deadline;
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP || task->status != TASK_WAITING || !task->sleep_armed) {
            continue;
        }
        if (task->sleep_deadline <= ex->now_ms) {
            wake_task(ex, task->id, 1);
        }
    }
    return 1;
}

static int next_ready(rt_executor* ex, uint64_t* out_id) {
    if (ex == NULL) {
        return 0;
    }
    while (!ready_pop(ex, out_id)) {
        if (!advance_time_to_next_timer(ex)) {
            return 0;
        }
    }
    return 1;
}

static uint64_t task_id_from_handle(void* task) {
    if (task == NULL) {
        panic_msg("invalid task handle");
        return 0;
    }
    return *(uint64_t*)task;
}

static void task_add_child(rt_task* parent, uint64_t child_id) {
    if (parent == NULL || child_id == 0) {
        return;
    }
    ensure_child_cap(parent, parent->children_len + 1);
    parent->children[parent->children_len++] = child_id;
}

static void scope_add_child(rt_scope* scope, uint64_t child_id) {
    if (scope == NULL || child_id == 0) {
        return;
    }
    ensure_scope_child_cap(scope, scope->children_len + 1);
    scope->children[scope->children_len++] = child_id;
}

static void cancel_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task->status == TASK_DONE) {
        return;
    }
    if (task->cancelled) {
        return;
    }
    task->cancelled = 1;
    if (task->status == TASK_WAITING) {
        wake_task(ex, task->id, 1);
    }
    for (size_t i = 0; i < task->children_len; i++) {
        cancel_task(ex, task->children[i]);
    }
}

static void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind, uint64_t result_bits) {
    if (ex == NULL || task == NULL) {
        return;
    }
    task->status = TASK_DONE;
    task->result_kind = result_kind;
    task->result_bits = result_bits;
    task->state = NULL;
    wake_key_all(ex, join_key(task->id));
    if (result_kind == TASK_RESULT_CANCELLED && task->parent_scope_id != 0) {
        rt_scope* scope = get_scope(ex, task->parent_scope_id);
        if (scope != NULL && scope->failfast && !scope->failfast_triggered) {
            scope->failfast_triggered = 1;
            if (scope->owner != 0) {
                cancel_task(ex, scope->owner);
            }
        }
    }
}

static poll_outcome poll_checkpoint_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->cancelled) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->checkpoint_polled) {
        out.kind = POLL_DONE_SUCCESS;
        return out;
    }
    task->checkpoint_polled = 1;
    out.kind = POLL_YIELDED;
    return out;
}

static poll_outcome poll_sleep_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->cancelled) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (!task->sleep_armed) {
        task->sleep_deadline = ex->now_ms + task->sleep_delay;
        task->sleep_armed = 1;
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    if (ex->now_ms < task->sleep_deadline) {
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    out.kind = POLL_DONE_SUCCESS;
    return out;
}

static int current_task_cancelled(rt_executor* ex) {
    if (ex == NULL || ex->current == 0) {
        return 0;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        return 0;
    }
    return task->cancelled != 0;
}

static poll_outcome poll_user_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    pending_key = waker_none();
    poll_result.kind = POLL_NONE;
    poll_result.park_key = waker_none();
    poll_result.state = NULL;
    poll_result.value_bits = 0;
    poll_active = 1;
    if (setjmp(poll_env) == 0) {
        __surge_poll_call((uint64_t)task->poll_fn_id);
        poll_active = 0;
        panic_msg("async poll returned without terminator");
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    poll_active = 0;
    return poll_result;
}

static poll_outcome poll_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->status == TASK_DONE) {
        out.kind = task->result_kind == TASK_RESULT_CANCELLED ? POLL_DONE_CANCELLED : POLL_DONE_SUCCESS;
        out.value_bits = task->result_bits;
        return out;
    }
    switch (task->kind) {
    case TASK_KIND_CHECKPOINT:
        return poll_checkpoint_task(ex, task);
    case TASK_KIND_SLEEP:
        return poll_sleep_task(ex, task);
    default:
        return poll_user_task(ex, task);
    }
}

static int run_ready_one(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t id = 0;
    if (!next_ready(ex, &id)) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        panic_msg("invalid task id");
        return 1;
    }
    ex->current = id;
    task->status = TASK_RUNNING;
    poll_outcome outcome = poll_task(ex, task);
    switch (outcome.kind) {
    case POLL_DONE_SUCCESS:
        mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
        break;
    case POLL_DONE_CANCELLED:
        mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
        break;
    case POLL_YIELDED:
        task->state = outcome.state;
        ready_push(ex, task->id);
        tick_virtual(ex);
        break;
    case POLL_PARKED:
        task->state = outcome.state;
        park_current(ex, outcome.park_key);
        break;
    default:
        panic_msg("async: unknown poll outcome");
        break;
    }
    ex->current = 0;
    return 1;
}

static void run_until_done(rt_executor* ex, uint64_t id, uint8_t* out_kind, uint64_t* out_bits) {
    if (ex == NULL || id == 0) {
        panic_msg("invalid task id");
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        panic_msg("invalid task id");
        return;
    }
    if (task->status != TASK_WAITING && task->status != TASK_DONE) {
        wake_task(ex, id, 1);
    }
    for (;;) {
        task = get_task(ex, id);
        if (task == NULL) {
            panic_msg("invalid task id");
            return;
        }
        if (task->status == TASK_DONE) {
            if (out_kind != NULL) {
                *out_kind = task->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            }
            if (out_bits != NULL) {
                *out_bits = task->result_bits;
            }
            return;
        }
        if (!run_ready_one(ex)) {
            panic_msg("async deadlock");
            return;
        }
    }
}

void* __task_create(void* poll_fn, void* state) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), sizeof(void*));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->poll_fn_id = (int64_t)(uintptr_t)poll_fn;
    task->state = state;
    task->status = TASK_READY;
    task->kind = TASK_KIND_USER;
    ex->tasks[id] = task;
    if (ex->current != 0) {
        rt_task* parent = get_task(ex, ex->current);
        task_add_child(parent, id);
    }
    ready_push(ex, id);

    uint64_t* handle = (uint64_t*)rt_alloc(sizeof(uint64_t), sizeof(uint64_t));
    if (handle == NULL) {
        panic_msg("async: task handle allocation failed");
        return NULL;
    }
    *handle = id;
    return handle;
}

void* __task_state(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || ex->current == 0) {
        panic_msg("async: __task_state without current task");
        return NULL;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        panic_msg("async: missing current task");
        return NULL;
    }
    void* state = task->state;
    task->state = NULL;
    return state;
}

void rt_task_wake(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t id = task_id_from_handle(task);
    wake_task(ex, id, 1);
}

uint8_t rt_task_poll(void* task, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    uint64_t target_id = task_id_from_handle(task);
    rt_task* target = get_task(ex, target_id);
    if (target == NULL) {
        panic_msg("async: invalid task handle");
        return 2;
    }
    if (ex->current == 0) {
        panic_msg("async poll outside task");
        return 2;
    }
    if (ex->current == target_id) {
        panic_msg("task cannot await itself");
        return 2;
    }
    if (current_task_cancelled(ex)) {
        return 0;
    }
    if (target->status != TASK_WAITING && target->status != TASK_DONE) {
        wake_task(ex, target_id, 1);
    }
    if (target->status == TASK_DONE) {
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        return target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
    }
    if (target->kind != TASK_KIND_CHECKPOINT) {
        pending_key = join_key(target_id);
    }
    return 0;
}

void rt_task_await(void* task, uint8_t* out_kind, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t id = task_id_from_handle(task);
    run_until_done(ex, id, out_kind, out_bits);
}

void rt_task_cancel(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t id = task_id_from_handle(task);
    cancel_task(ex, id);
}

void* rt_task_clone(void* task) {
    if (task == NULL) {
        panic_msg("invalid task handle");
        return NULL;
    }
    uint64_t id = task_id_from_handle(task);
    uint64_t* handle = (uint64_t*)rt_alloc(sizeof(uint64_t), sizeof(uint64_t));
    if (handle == NULL) {
        panic_msg("async: task handle allocation failed");
        return NULL;
    }
    *handle = id;
    return handle;
}

void rt_async_yield(void* state) {
    if (!poll_active) {
        panic_msg("async_yield outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = 0;
    if (current_task_cancelled(&exec_state)) {
        poll_result.kind = POLL_DONE_CANCELLED;
        poll_result.park_key = waker_none();
        pending_key = waker_none();
        longjmp(poll_env, 1);
    }
    if (waker_valid(pending_key)) {
        poll_result.kind = POLL_PARKED;
        poll_result.park_key = pending_key;
    } else {
        poll_result.kind = POLL_YIELDED;
        poll_result.park_key = waker_none();
    }
    pending_key = waker_none();
    longjmp(poll_env, 1);
}

void rt_async_return(void* state, uint64_t bits) {
    if (!poll_active) {
        panic_msg("async_return outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = bits;
    poll_result.kind = POLL_DONE_SUCCESS;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(poll_env, 1);
}

void rt_async_return_cancelled(void* state) {
    if (!poll_active) {
        panic_msg("async_cancel outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = 0;
    poll_result.kind = POLL_DONE_CANCELLED;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(poll_env, 1);
}

void* rt_scope_enter(bool failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || ex->current == 0) {
        panic_msg("rt_scope_enter without current task");
        return NULL;
    }
    uint64_t id = ex->next_scope_id++;
    ensure_scope_cap(ex, id);
    rt_scope* scope = (rt_scope*)rt_alloc(sizeof(rt_scope), sizeof(void*));
    if (scope == NULL) {
        panic_msg("async: scope allocation failed");
        return NULL;
    }
    memset(scope, 0, sizeof(rt_scope));
    scope->id = id;
    scope->owner = ex->current;
    scope->failfast = failfast ? 1 : 0;
    scope->failfast_triggered = 0;
    ex->scopes[id] = scope;
    rt_task* owner = get_task(ex, ex->current);
    if (owner != NULL) {
        owner->scope_id = id;
    }
    return (void*)(uintptr_t)id;
}

void rt_scope_register_child(void* scope_handle, void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    uint64_t child_id = task_id_from_handle(task);
    scope_add_child(scope, child_id);
    rt_task* child = get_task(ex, child_id);
    if (child != NULL) {
        child->parent_scope_id = scope_id;
    }
}

void rt_scope_cancel_all(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        cancel_task(ex, scope->children[i]);
    }
}

bool rt_scope_join_all(void* scope_handle, uint64_t* pending, bool* failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return true;
    }
    if (failfast != NULL) {
        *failfast = scope->failfast_triggered ? true : false;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        uint64_t child_id = scope->children[i];
        rt_task* child = get_task(ex, child_id);
        if (child == NULL || child->status == TASK_DONE) {
            continue;
        }
        if (pending != NULL) {
            *pending = child_id;
        }
        pending_key = join_key(child_id);
        return false;
    }
    return true;
}

void rt_scope_exit(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    if (scope->owner != 0) {
        rt_task* owner = get_task(ex, scope->owner);
        if (owner != NULL && owner->scope_id == scope_id) {
            owner->scope_id = 0;
        }
    }
    ex->scopes[scope_id] = NULL;
}

void* checkpoint(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), sizeof(void*));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->status = TASK_READY;
    task->kind = TASK_KIND_CHECKPOINT;
    ex->tasks[id] = task;
    ready_push(ex, id);

    uint64_t* handle = (uint64_t*)rt_alloc(sizeof(uint64_t), sizeof(uint64_t));
    if (handle == NULL) {
        panic_msg("async: task handle allocation failed");
        return NULL;
    }
    *handle = id;
    return handle;
}

void* sleep(void* ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t delay = (uint64_t)(uintptr_t)ms;
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), sizeof(void*));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->status = TASK_READY;
    task->kind = TASK_KIND_SLEEP;
    task->sleep_delay = delay;
    ex->tasks[id] = task;
    ready_push(ex, id);

    uint64_t* handle = (uint64_t*)rt_alloc(sizeof(uint64_t), sizeof(uint64_t));
    if (handle == NULL) {
        panic_msg("async: task handle allocation failed");
        return NULL;
    }
    *handle = id;
    return handle;
}
