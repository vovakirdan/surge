#include "rt_async_internal.h"

// Async runtime state, queues, and memory helpers.

rt_executor exec_state;
jmp_buf poll_env;
int poll_active = 0;
poll_outcome poll_result;
waker_key pending_key;

void panic_msg(const char* msg) {
    if (msg == NULL) {
        return;
    }
    rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
}

waker_key waker_none(void) {
    waker_key key = {WAKER_NONE, 0};
    return key;
}

int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

waker_key join_key(uint64_t id) {
    waker_key key = {WAKER_JOIN, id};
    return key;
}

waker_key timer_key(uint64_t id) {
    waker_key key = {WAKER_TIMER, id};
    return key;
}

rt_executor* ensure_exec(void) {
    rt_executor* ex = &exec_state;
    if (ex->next_id == 0) {
        ex->next_id = 1;
    }
    if (ex->next_scope_id == 0) {
        ex->next_scope_id = 1;
    }
    return ex;
}

rt_task* get_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0 || id >= ex->tasks_cap) {
        return NULL;
    }
    return ex->tasks[id];
}

rt_scope* get_scope(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0 || id >= ex->scopes_cap) {
        return NULL;
    }
    return ex->scopes[id];
}

void ensure_task_cap(rt_executor* ex, uint64_t id) {
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
    rt_task** next = (rt_task**)rt_realloc((uint8_t*)ex->tasks, (uint64_t)old_size, (uint64_t)new_size, _Alignof(rt_task*));
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

void ensure_scope_cap(rt_executor* ex, uint64_t id) {
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
    rt_scope** next = (rt_scope**)rt_realloc((uint8_t*)ex->scopes, (uint64_t)old_size, (uint64_t)new_size, _Alignof(rt_scope*));
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

void ensure_ready_cap(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    if (ex->ready_len < ex->ready_cap) {
        return;
    }
    size_t next_cap = ex->ready_cap == 0 ? 16 : ex->ready_cap * 2;
    size_t old_size = ex->ready_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)ex->ready, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: ready queue allocation failed");
        return;
    }
    ex->ready = next;
    ex->ready_cap = next_cap;
}

void ensure_waiter_cap(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    if (ex->waiters_len < ex->waiters_cap) {
        return;
    }
    size_t next_cap = ex->waiters_cap == 0 ? 16 : ex->waiters_cap * 2;
    size_t old_size = ex->waiters_cap * sizeof(waiter);
    size_t new_size = next_cap * sizeof(waiter);
    waiter* next = (waiter*)rt_realloc((uint8_t*)ex->waiters, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waiter));
    if (next == NULL) {
        panic_msg("async: waiter allocation failed");
        return;
    }
    ex->waiters = next;
    ex->waiters_cap = next_cap;
}

void ensure_child_cap(rt_task* task, size_t want) {
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
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)task->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: child allocation failed");
        return;
    }
    task->children = next;
    task->children_cap = next_cap;
}

void ensure_scope_child_cap(rt_scope* scope, size_t want) {
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
    uint64_t* next = (uint64_t*)rt_realloc((uint8_t*)scope->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: scope child allocation failed");
        return;
    }
    scope->children = next;
    scope->children_cap = next_cap;
}

void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
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

void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    ensure_waiter_cap(ex);
    ex->waiters[ex->waiters_len++] = (waiter){key, task_id};
}

void ready_push(rt_executor* ex, uint64_t id) {
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

int ready_pop(rt_executor* ex, uint64_t* out_id) {
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

void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag) {
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

void wake_key_all(rt_executor* ex, waker_key key) {
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

void park_current(rt_executor* ex, waker_key key) {
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

void tick_virtual(rt_executor* ex) {
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

int advance_time_to_next_timer(rt_executor* ex) {
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

int next_ready(rt_executor* ex, uint64_t* out_id) {
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

rt_task* task_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("invalid task handle");
        return NULL;
    }
    return (rt_task*)handle;
}

uint64_t task_id_from_handle(void* handle) {
    rt_task* task = task_from_handle(handle);
    if (task == NULL) {
        return 0;
    }
    return task->id;
}

void task_add_child(rt_task* parent, uint64_t child_id) {
    if (parent == NULL || child_id == 0) {
        return;
    }
    ensure_child_cap(parent, parent->children_len + 1);
    parent->children[parent->children_len++] = child_id;
}

void scope_add_child(rt_scope* scope, uint64_t child_id) {
    if (scope == NULL || child_id == 0) {
        return;
    }
    ensure_scope_child_cap(scope, scope->children_len + 1);
    scope->children[scope->children_len++] = child_id;
}

void task_add_ref(rt_task* task) {
    if (task == NULL) {
        return;
    }
    task->handle_refs++;
}

static void free_task(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task->children != NULL && task->children_cap > 0) {
        rt_free((uint8_t*)task->children, (uint64_t)(task->children_cap * sizeof(uint64_t)), _Alignof(uint64_t));
    }
    if (task->id < ex->tasks_cap) {
        ex->tasks[task->id] = NULL;
    }
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
}

void task_release(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task->handle_refs == 0) {
        return;
    }
    task->handle_refs--;
    if (task->handle_refs == 0 && task->status == TASK_DONE) {
        free_task(ex, task);
    }
}

int current_task_cancelled(rt_executor* ex) {
    if (ex == NULL || ex->current == 0) {
        return 0;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        return 0;
    }
    return task->cancelled != 0;
}

void cancel_task(rt_executor* ex, uint64_t id) {
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

void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind, uint64_t result_bits) {
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
    if (task->handle_refs == 0) {
        free_task(ex, task);
    }
}
