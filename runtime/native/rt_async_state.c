#include "rt_async_internal.h"

#include <limits.h>
#include <signal.h>
#include <stdlib.h>
#include <unistd.h>

// Async runtime state, queues, and memory helpers.
//
// MT NOTES (iteration 1):
// - ST executor stored tasks in exec_state.tasks and scheduled via exec_state.ready.
// - A poll sets pending_key, then rt_async_yield parks via park_current and waiters list.
// - Cancellation is observed in rt_async_yield/current_task_cancelled.
// - MT needs a wake token to avoid wake-before-park races and a dedicated I/O thread
//   (workers must not block on poll()).
// - poll_net_waiters previously blocked inline; MT uses an I/O thread plus bounded poll timeouts
//   to avoid starving newly added waiters.
// - ready_push skips RUNNING tasks; yielded tasks must set READY before requeue to avoid drops.
// - task release/free touches shared waiters/queues, so executor state remains under one lock.
// - virtual time still advances on yields; timers only fast-forward when the system is idle.

rt_executor exec_state;
_Thread_local jmp_buf poll_env;
_Thread_local int poll_active = 0;
_Thread_local poll_outcome poll_result;
_Thread_local waker_key pending_key;
_Thread_local uint64_t tls_current_id;
_Thread_local rt_task* tls_current_task;
static pthread_once_t exec_once = PTHREAD_ONCE_INIT;

static volatile sig_atomic_t trace_exec_enabled_flag = 0;
static _Atomic uint64_t trace_wake_called_total;
static _Atomic uint64_t trace_wake_enqueued_total;
static _Atomic uint64_t trace_wake_ignored_completed_total;
static _Atomic uint64_t trace_park_attempt_total;
static _Atomic uint64_t trace_park_committed_total;
static _Atomic uint64_t trace_worker_sleep_total;
static _Atomic uint64_t trace_worker_wake_total;

static int trace_exec_enabled(void) {
    return trace_exec_enabled_flag != 0;
}

static void trace_exec_inc(_Atomic uint64_t* counter) {
    if (!trace_exec_enabled() || counter == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

static size_t trace_exec_append_literal(char* buf, size_t pos, size_t cap, const char* lit) {
    if (buf == NULL || lit == NULL) {
        return pos;
    }
    for (size_t i = 0; lit[i] != '\0' && pos + 1 < cap; i++) {
        buf[pos++] = lit[i];
    }
    return pos;
}

static size_t trace_exec_append_u64(char* buf, size_t pos, size_t cap, uint64_t value) {
    char tmp[32];
    size_t len = 0;
    do {
        tmp[len++] = (char)('0' + (value % 10));
        value /= 10;
    } while (value > 0 && len < sizeof(tmp));
    for (size_t i = 0; i < len && pos + 1 < cap; i++) {
        buf[pos++] = tmp[len - 1 - i];
    }
    return pos;
}

static void trace_exec_dump(const char* reason) {
    if (!trace_exec_enabled()) {
        return;
    }
    char buf[512];
    size_t pos = 0;
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), "TRACE_EXEC ");
    if (reason != NULL) {
        pos = trace_exec_append_literal(buf, pos, sizeof(buf), "reason=");
        pos = trace_exec_append_literal(buf, pos, sizeof(buf), reason);
        pos = trace_exec_append_literal(buf, pos, sizeof(buf), " ");
    }
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), "wake_called=");
    pos =
        trace_exec_append_u64(buf,
                              pos,
                              sizeof(buf),
                              atomic_load_explicit(&trace_wake_called_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " wake_enqueued=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_wake_enqueued_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " wake_ignored_completed=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_wake_ignored_completed_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " park_attempt=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_park_attempt_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " park_committed=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_park_committed_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " worker_sleep=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_worker_sleep_total, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " worker_wake=");
    pos =
        trace_exec_append_u64(buf,
                              pos,
                              sizeof(buf),
                              atomic_load_explicit(&trace_worker_wake_total, memory_order_relaxed));
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}

static void trace_exec_signal_handler(int sig) {
    (void)sig;
    trace_exec_dump("sigusr1");
}

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

waker_key scope_key(uint64_t id) {
    waker_key key = {WAKER_SCOPE, id};
    return key;
}

waker_key channel_send_key(rt_channel* ch) {
    waker_key key = {WAKER_CHAN_SEND, (uint64_t)(uintptr_t)ch};
    return key;
}

waker_key channel_recv_key(rt_channel* ch) {
    waker_key key = {WAKER_CHAN_RECV, (uint64_t)(uintptr_t)ch};
    return key;
}

waker_key net_accept_key(int fd) {
    waker_key key = {WAKER_NET_ACCEPT, (uint64_t)fd};
    return key;
}

waker_key net_read_key(int fd) {
    waker_key key = {WAKER_NET_READ, (uint64_t)fd};
    return key;
}

waker_key net_write_key(int fd) {
    waker_key key = {WAKER_NET_WRITE, (uint64_t)fd};
    return key;
}

uint64_t rt_current_task_id(void) {
    if (tls_current_task != NULL) {
        return tls_current_task->id;
    }
    return tls_current_id;
}

rt_task* rt_current_task(void) {
    return tls_current_task;
}

void rt_set_current_task(rt_task* task) {
    tls_current_task = task;
    tls_current_id = task != NULL ? task->id : 0;
}

void rt_lock(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    pthread_mutex_lock(&ex->lock);
}

void rt_unlock(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    pthread_mutex_unlock(&ex->lock);
}

static uint32_t rt_env_worker_count(void) {
    const char* value = getenv("SURGE_THREADS");
    if (value == NULL || value[0] == '\0') {
        return 0;
    }
    char* end = NULL;
    long parsed = strtol(value, &end, 10);
    if (end == value || parsed <= 0) {
        return 0;
    }
    if ((unsigned long)parsed > UINT32_MAX) { // NOLINT(runtime/int)
        return UINT32_MAX;
    }
    return (uint32_t)parsed;
}

static uint32_t rt_detect_cpu_count(void) {
    long cpus = sysconf(_SC_NPROCESSORS_ONLN);
    if (cpus <= 0) {
        return 1;
    }
    if (cpus > (long)UINT32_MAX) { // NOLINT(runtime/int)
        return UINT32_MAX;
    }
    return (uint32_t)cpus;
}

static uint32_t rt_default_worker_count(void) {
    uint32_t cpus = rt_detect_cpu_count();
    if (cpus < 2) {
        return 2;
    }
    return cpus;
}

static void rt_start_workers(rt_executor* ex);
static void* rt_worker_main(void* arg);
static void* rt_io_main(void* arg);
static void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome);
static int ready_is_empty(const rt_executor* ex);
static void trace_exec_init(void);

static void exec_init_once(void) {
    rt_executor* ex = &exec_state;
    memset(ex, 0, sizeof(*ex));
    ex->next_id = 1;
    ex->next_scope_id = 1;
    pthread_mutex_init(&ex->lock, NULL);
    pthread_cond_init(&ex->ready_cv, NULL);
    pthread_cond_init(&ex->io_cv, NULL);
    pthread_cond_init(&ex->done_cv, NULL);
    trace_exec_init();
    uint32_t threads = rt_env_worker_count();
    if (threads == 0) {
        threads = rt_default_worker_count();
    }
    ex->worker_count = threads;
    if (ex->worker_count > 1) {
        rt_start_workers(ex);
    }
}

rt_executor* ensure_exec(void) {
    pthread_once(&exec_once, exec_init_once);
    return &exec_state;
}

uint64_t rt_worker_count(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 0;
    }
    return (uint64_t)ex->worker_count;
}

static int ready_is_empty(const rt_executor* ex) {
    if (ex == NULL) {
        return 1;
    }
    return ex->ready_head >= ex->ready_len;
}

static void trace_exec_init(void) {
    const char* value = getenv("SURGE_TRACE_EXEC");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return;
    }
    trace_exec_enabled_flag = 1;
#ifdef SIGUSR1
    (void)signal(SIGUSR1, trace_exec_signal_handler);
#endif
}

static void rt_start_workers(rt_executor* ex) {
    if (ex == NULL || ex->worker_count <= 1) {
        return;
    }
    uint32_t count = ex->worker_count;
    size_t total = (size_t)count + 1;
    pthread_t* threads =
        (pthread_t*)rt_alloc((uint64_t)(total * sizeof(pthread_t)), _Alignof(pthread_t));
    if (threads == NULL) {
        panic_msg("async: worker allocation failed");
        return;
    }
    ex->workers = threads;
    if (pthread_create(&threads[0], NULL, rt_io_main, ex) != 0) {
        panic_msg("async: io worker start failed");
        return;
    }
    (void)pthread_detach(threads[0]);
    ex->io_started = 1;
    for (uint32_t i = 0; i < count; i++) {
        if (pthread_create(&threads[i + 1], NULL, rt_worker_main, ex) != 0) {
            panic_msg("async: worker start failed");
            return;
        }
        (void)pthread_detach(threads[i + 1]);
    }
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

static int ensure_ptr_array_cap(void** array,
                                size_t elem_size,
                                size_t* cap,
                                size_t want,
                                uint64_t align,
                                const char* overflow_msg,
                                const char* alloc_msg) {
    if (array == NULL || cap == NULL || elem_size == 0) {
        panic_msg(overflow_msg);
        return 0;
    }
    if (want <= *cap) {
        return 1;
    }
    if (*cap > SIZE_MAX / elem_size) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t next_cap = *cap == 0 ? 8 : *cap;
    while (next_cap < want) {
        if (next_cap > SIZE_MAX / 2) {
            panic_msg(overflow_msg);
            return 0;
        }
        next_cap *= 2;
    }
    if (next_cap > SIZE_MAX / elem_size) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t old_size = (*cap) * elem_size;
    size_t new_size = next_cap * elem_size;
    size_t diff = next_cap - *cap;
    if (diff > 0 && diff > SIZE_MAX / elem_size) {
        panic_msg(overflow_msg);
        return 0;
    }
    if (old_size > UINT64_MAX || new_size > UINT64_MAX) {
        panic_msg(overflow_msg);
        return 0;
    }
    void* next = rt_realloc((uint8_t*)(*array), (uint64_t)old_size, (uint64_t)new_size, align);
    if (next == NULL) {
        panic_msg(alloc_msg);
        return 0;
    }
    if (diff > 0) {
        memset((uint8_t*)next + old_size, 0, diff * elem_size);
    }
    *array = next;
    *cap = next_cap;
    return 1;
}

void ensure_task_cap(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return;
    }
    if (id < ex->tasks_cap) {
        return;
    }
    if (id >= SIZE_MAX) {
        panic_msg("async: task capacity overflow");
        return;
    }
    size_t want = (size_t)id + 1;
    (void)ensure_ptr_array_cap((void**)&ex->tasks,
                               sizeof(rt_task*),
                               &ex->tasks_cap,
                               want,
                               _Alignof(rt_task*),
                               "async: task capacity overflow",
                               "async: task allocation failed");
}

void ensure_scope_cap(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return;
    }
    if (id < ex->scopes_cap) {
        return;
    }
    if (id >= SIZE_MAX) {
        panic_msg("async: scope capacity overflow");
        return;
    }
    size_t want = (size_t)id + 1;
    (void)ensure_ptr_array_cap((void**)&ex->scopes,
                               sizeof(rt_scope*),
                               &ex->scopes_cap,
                               want,
                               _Alignof(rt_scope*),
                               "async: scope capacity overflow",
                               "async: scope allocation failed");
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
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)ex->ready, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
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
    waiter* next = (waiter*)rt_realloc(
        (uint8_t*)ex->waiters, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waiter));
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
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)task->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
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
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)scope->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: scope child allocation failed");
        return;
    }
    scope->children = next;
    scope->children_cap = next_cap;
}

static void ensure_wait_keys_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->wait_keys_cap >= want) {
        return;
    }
    size_t next_cap = task->wait_keys_cap == 0 ? 4 : task->wait_keys_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->wait_keys_cap * sizeof(waker_key);
    size_t new_size = next_cap * sizeof(waker_key);
    waker_key* next = (waker_key*)rt_realloc(
        (uint8_t*)task->wait_keys, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waker_key));
    if (next == NULL) {
        panic_msg("async: wait key allocation failed");
        return;
    }
    task->wait_keys = next;
    task->wait_keys_cap = next_cap;
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

void clear_wait_keys(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->wait_keys_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->wait_keys_len; i++) {
        remove_waiter(ex, task->wait_keys[i], task->id);
    }
    task->wait_keys_len = 0;
}

void clear_select_timers(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->select_timers_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->select_timers_len; i++) {
        uint64_t timer_id = task->select_timers[i];
        if (timer_id == 0) {
            continue;
        }
        rt_task* timer = get_task(ex, timer_id);
        if (timer != NULL) {
            cancel_task(ex, timer_id);
            task_release(ex, timer);
        }
        task->select_timers[i] = 0;
    }
    task->select_timers_len = 0;
}

void add_wait_key(rt_executor* ex, rt_task* task, waker_key key) {
    if (ex == NULL || task == NULL || !waker_valid(key)) {
        return;
    }
    ensure_wait_keys_cap(task, task->wait_keys_len + 1);
    if (task->wait_keys == NULL) {
        return;
    }
    task->wait_keys[task->wait_keys_len++] = key;
    add_waiter(ex, key, task->id);
}

// NOTES (MT iteration 2):
// - prepare_park pre-registers waiters under ex->lock to avoid wake-before-park races for user
// tasks.
// - Channel waiters now share the executor waiters list (FIFO per key via pop_waiter), so wake is
// O(n).
// - Documented primitives like Semaphore/Condition/Mutex/RwLock have no native runtime impl yet.
void prepare_park(rt_executor* ex, rt_task* task, waker_key key, int already_added) {
    if (ex == NULL || task == NULL || !waker_valid(key)) {
        return;
    }
    if (!already_added) {
        if (!(task->park_prepared && task->park_key.kind == key.kind &&
              task->park_key.id == key.id)) {
            add_waiter(ex, key, task->id);
        }
    }
    task->park_key = key;
    task->park_prepared = 1;
}

int pop_waiter(rt_executor* ex, waker_key key, uint64_t* out_id) {
    if (ex == NULL || !waker_valid(key) || ex->waiters_len == 0) {
        return 0;
    }
    size_t out = 0;
    int found = 0;
    uint64_t found_id = 0;
    for (size_t i = 0; i < ex->waiters_len; i++) {
        waiter w = ex->waiters[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            const rt_task* task = get_task(ex, w.task_id);
            if (task == NULL || task_status_load(task) == TASK_DONE ||
                task_cancelled_load(task) != 0) {
                continue;
            }
            if (!found) {
                found = 1;
                found_id = w.task_id;
                continue;
            }
        }
        ex->waiters[out++] = w;
    }
    ex->waiters_len = out;
    if (found && out_id != NULL) {
        *out_id = found_id;
    }
    return found;
}

static int ready_push_inner(rt_executor* ex, uint64_t id) {
    if (ex == NULL) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    uint8_t status = task_status_load(task);
    if (task == NULL || status == TASK_DONE) {
        return 0;
    }
    if (status == TASK_RUNNING) {
        return 0;
    }
    if (task_enqueued_load(task) != 0) {
        return 0;
    }
    ensure_ready_cap(ex);
    ex->ready[ex->ready_len++] = id;
    task_enqueued_store(task, 1);
    task_status_store(task, TASK_READY);
    pthread_cond_signal(&ex->ready_cv);
    return 1;
}

void ready_push(rt_executor* ex, uint64_t id) {
    (void)ready_push_inner(ex, id);
}

int ready_pop(rt_executor* ex, uint64_t* out_id) {
    if (ex == NULL) {
        return 0;
    }
    while (ex->ready_head < ex->ready_len) {
        uint64_t id = ex->ready[ex->ready_head++];
        rt_task* task = get_task(ex, id);
        uint8_t status = task_status_load(task);
        if (task == NULL || status == TASK_DONE || status == TASK_RUNNING) {
            continue;
        }
        task_enqueued_store(task, 0);
        if (out_id != NULL) {
            *out_id = id;
        }
        if (ex->ready_head > 0 && ex->ready_head == ex->ready_len) {
            ex->ready_head = 0;
            ex->ready_len = 0;
        }
        return 1;
    }
    return 0;
}

void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag) {
    if (ex == NULL) {
        return;
    }
    trace_exec_inc(&trace_wake_called_total);
    rt_task* task = get_task(ex, id);
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        trace_exec_inc(&trace_wake_ignored_completed_total);
        return;
    }
    if (remove_waiter_flag && waker_valid(task->park_key)) {
        remove_waiter(ex, task->park_key, id);
    }
    task->park_key = waker_none();
    task->park_prepared = 0;
    (void)task_wake_token_exchange(task, 1);
    if (ready_push_inner(ex, id)) {
        trace_exec_inc(&trace_wake_enqueued_total);
    }
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
    if (ex == NULL || !waker_valid(key) || rt_current_task_id() == 0) {
        return;
    }
    rt_task* task = rt_current_task();
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        return;
    }
    trace_exec_inc(&trace_park_attempt_total);
    if (task_wake_token_exchange(task, 0) != 0) {
        task->park_prepared = 0;
        task->park_key = waker_none();
        task_status_store(task, TASK_READY);
        ready_push(ex, task->id);
        return;
    }
    task_status_store(task, TASK_WAITING);
    if (!(task->park_prepared && task->park_key.kind == key.kind && task->park_key.id == key.id)) {
        task->park_key = key;
        add_waiter(ex, key, task->id);
    }
    task->park_prepared = 0;
    if (task_wake_token_exchange(task, 0) != 0) {
        remove_waiter(ex, key, task->id);
        task->park_key = waker_none();
        task_status_store(task, TASK_READY);
        ready_push(ex, task->id);
        return;
    }
    trace_exec_inc(&trace_park_committed_total);
    pthread_cond_signal(&ex->io_cv);
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
        const rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP ||
            task_status_load(task) != TASK_WAITING || !task->sleep_armed) {
            continue;
        }
        if (task->sleep_deadline <= ex->now_ms) {
            wake_task(ex, task->id, 1);
        }
    }
}

static int has_net_waiters(const rt_executor* ex) {
    if (ex == NULL || ex->waiters_len == 0) {
        return 0;
    }
    for (size_t i = 0; i < ex->waiters_len; i++) {
        waker_kind kind = (waker_kind)ex->waiters[i].key.kind;
        if (kind == WAKER_NET_ACCEPT || kind == WAKER_NET_READ || kind == WAKER_NET_WRITE) {
            return 1;
        }
    }
    return 0;
}

static int next_sleep_deadline(const rt_executor* ex, uint64_t* out_deadline) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t next_deadline = UINT64_MAX;
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        const rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP ||
            task_status_load(task) != TASK_WAITING || !task->sleep_armed) {
            continue;
        }
        if (task->sleep_deadline < next_deadline) {
            next_deadline = task->sleep_deadline;
        }
    }
    if (next_deadline == UINT64_MAX) {
        return 0;
    }
    if (out_deadline != NULL) {
        *out_deadline = next_deadline;
    }
    return 1;
}

int advance_time_to_next_timer(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t next_deadline = 0;
    if (!next_sleep_deadline(ex, &next_deadline)) {
        return 0;
    }
    ex->now_ms = next_deadline;
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        const rt_task* task = ex->tasks[i];
        if (task == NULL || task->kind != TASK_KIND_SLEEP ||
            task_status_load(task) != TASK_WAITING || !task->sleep_armed) {
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
        if (poll_net_waiters(ex, 0)) {
            continue;
        }
        uint64_t next_deadline = 0;
        int have_timer = next_sleep_deadline(ex, &next_deadline);
        if (have_timer) {
            if (has_net_waiters(ex)) {
                uint64_t now = ex->now_ms;
                uint64_t diff = next_deadline > now ? next_deadline - now : 0;
                int timeout_ms = diff > (uint64_t)INT_MAX ? INT_MAX : (int)diff;
                if (timeout_ms > 0) {
                    if (poll_net_waiters(ex, timeout_ms)) {
                        continue;
                    }
                }
                if (advance_time_to_next_timer(ex)) {
                    continue;
                }
            } else if (advance_time_to_next_timer(ex)) {
                continue;
            }
        } else {
            if (poll_net_waiters(ex, -1)) {
                continue;
            }
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
    const rt_task* task = task_from_handle(handle);
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

void scope_cancel_children_locked(rt_executor* ex, const rt_scope* scope) {
    if (ex == NULL || scope == NULL) {
        return;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        cancel_task(ex, scope->children[i]);
    }
}

void scope_child_done_locked(rt_executor* ex, rt_scope* scope) {
    if (ex == NULL || scope == NULL) {
        return;
    }
    if (scope->active_children > 0) {
        scope->active_children--;
    }
    if (scope->active_children == 0) {
        wake_key_all(ex, scope_key(scope->id));
    }
}

void task_add_ref(rt_task* task) {
    if (task == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(&task->handle_refs, 1, memory_order_relaxed);
}

static void free_task(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task->wait_keys_len > 0) {
        clear_wait_keys(ex, task);
    }
    if (task->wait_keys != NULL && task->wait_keys_cap > 0) {
        rt_free((uint8_t*)task->wait_keys,
                (uint64_t)(task->wait_keys_cap * sizeof(waker_key)),
                _Alignof(waker_key));
    }
    if (task->select_timers != NULL && task->select_timers_cap > 0) {
        rt_free((uint8_t*)task->select_timers,
                (uint64_t)(task->select_timers_cap * sizeof(uint64_t)),
                _Alignof(uint64_t));
    }
    if (task->children != NULL && task->children_cap > 0) {
        rt_free((uint8_t*)task->children,
                (uint64_t)(task->children_cap * sizeof(uint64_t)),
                _Alignof(uint64_t));
    }
    if (task->id < ex->tasks_cap) {
        ex->tasks[task->id] = NULL;
    }
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
}

void task_release(rt_executor* ex, rt_task* task) {
    // Caller must hold ex->lock.
    if (ex == NULL || task == NULL) {
        return;
    }
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if (refs == 0) {
        return;
    }
    refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    if (refs == 1 && task_status_load(task) == TASK_DONE) {
        free_task(ex, task);
    }
}

int current_task_cancelled(rt_executor* ex) {
    (void)ex;
    const rt_task* task = rt_current_task();
    return task != NULL && task_cancelled_load(task) != 0;
}

void cancel_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        return;
    }
    if (task_cancelled_load(task) != 0) {
        return;
    }
    task_cancelled_store(task, 1);
    if (task_status_load(task) == TASK_WAITING) {
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
    if (task->wait_keys_len > 0) {
        clear_wait_keys(ex, task);
    }
    if (task->select_timers_len > 0) {
        clear_select_timers(ex, task);
    }
    if (waker_valid(task->park_key)) {
        remove_waiter(ex, task->park_key, task->id);
    }
    task->park_key = waker_none();
    task->park_prepared = 0;
    task_status_store(task, TASK_DONE);
    task_enqueued_store(task, 0);
    task->result_kind = result_kind;
    task->result_bits = result_bits;
    task->state = NULL;
    rt_scope* scope = NULL;
    if (task->parent_scope_id != 0) {
        scope = get_scope(ex, task->parent_scope_id);
    }
    if (scope != NULL) {
        if (result_kind == TASK_RESULT_CANCELLED && scope->failfast && !scope->failfast_triggered) {
            // First cancellation observed under the executor lock wins.
            scope->failfast_triggered = 1;
            scope->failfast_child = task->id;
            // First cancellation wins; cancel remaining children and wake the owner.
            scope_cancel_children_locked(ex, scope);
            if (scope->owner != 0) {
                wake_task(ex, scope->owner, 1);
            }
        }
        if (task->scope_registered) {
            scope_child_done_locked(ex, scope);
            task->scope_registered = 0;
        }
    }
    wake_key_all(ex, join_key(task->id));
    pthread_cond_broadcast(&ex->done_cv);
    if (atomic_load_explicit(&task->handle_refs, memory_order_relaxed) == 0) {
        free_task(ex, task);
    }
}

static void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome) {
    if (ex == NULL || task == NULL) {
        return;
    }
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
            break;
        case POLL_DONE_CANCELLED:
            if (task->scope_id != 0) {
                rt_scope* scope = get_scope(ex, task->scope_id);
                if (scope != NULL) {
                    if (scope->active_children > 0) {
                        task->cancel_pending = 1;
                        scope_cancel_children_locked(ex, scope);
                        task->state = outcome.state;
                        waker_key key = scope_key(scope->id);
                        prepare_park(ex, task, key, 0);
                        park_current(ex, key);
                        break;
                    }
                    scope_exit_locked(ex, scope);
                }
            }
            mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
            break;
        case POLL_YIELDED:
            task->state = outcome.state;
            task_status_store(task, TASK_READY);
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
}

static void* rt_worker_main(void* arg) {
    rt_executor* ex = (rt_executor*)arg;
    if (ex == NULL) {
        return NULL;
    }
    rt_set_current_task(NULL);
    for (;;) {
        rt_lock(ex);
        while (!ex->shutdown && ready_is_empty(ex)) {
            trace_exec_inc(&trace_worker_sleep_total);
            pthread_cond_wait(&ex->ready_cv, &ex->lock);
            trace_exec_inc(&trace_worker_wake_total);
        }
        if (ex->shutdown) {
            rt_unlock(ex);
            break;
        }
        uint64_t id = 0;
        if (!ready_pop(ex, &id)) {
            rt_unlock(ex);
            continue;
        }
        rt_task* task = get_task(ex, id);
        if (task == NULL || task_status_load(task) == TASK_DONE) {
            rt_unlock(ex);
            continue;
        }
        task_status_store(task, TASK_RUNNING);
        (void)task_wake_token_exchange(task, 0);
        ex->running_count++;
        rt_set_current_task(task);

        uint8_t kind = task->kind;
        if (kind != TASK_KIND_USER) {
            task_polling_enter(task);
            poll_outcome outcome = poll_task(ex, task);
            task_polling_exit(task);
            ex->running_count--;
            apply_poll_outcome(ex, task, outcome);
            rt_set_current_task(NULL);
            if (ex->running_count == 0 && ready_is_empty(ex)) {
                pthread_cond_signal(&ex->io_cv);
            }
            rt_unlock(ex);
            continue;
        }
        rt_unlock(ex);

        task_polling_enter(task);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);

        rt_lock(ex);
        ex->running_count--;
        apply_poll_outcome(ex, task, outcome);
        rt_set_current_task(NULL);
        if (ex->running_count == 0 && ready_is_empty(ex)) {
            pthread_cond_signal(&ex->io_cv);
        }
        rt_unlock(ex);
    }
    rt_set_current_task(NULL);
    return NULL;
}

static void* rt_io_main(void* arg) {
    rt_executor* ex = (rt_executor*)arg;
    if (ex == NULL) {
        return NULL;
    }
    const int poll_slice_ms = 50;
    rt_lock(ex);
    for (;;) {
        if (ex->shutdown) {
            break;
        }
        uint64_t deadline = 0;
        int have_timer = next_sleep_deadline(ex, &deadline);
        int have_net = has_net_waiters(ex);
        int idle = ex->running_count == 0 && ready_is_empty(ex);

        if (!have_net && (!have_timer || !idle)) {
            pthread_cond_wait(&ex->io_cv, &ex->lock);
            continue;
        }

        if (!have_net) {
            if (idle && have_timer && advance_time_to_next_timer(ex)) {
                continue;
            }
            pthread_cond_wait(&ex->io_cv, &ex->lock);
            continue;
        }

        int timeout_ms = poll_slice_ms;
        if (idle && have_timer) {
            uint64_t now = ex->now_ms;
            uint64_t diff = deadline > now ? deadline - now : 0;
            int timer_ms = diff > (uint64_t)INT_MAX ? INT_MAX : (int)diff;
            if (timer_ms < timeout_ms) {
                timeout_ms = timer_ms;
            }
        }
        if (timeout_ms < 0) {
            timeout_ms = poll_slice_ms;
        }
        if (poll_net_waiters(ex, timeout_ms)) {
            continue;
        }
        if (idle && have_timer) {
            if (advance_time_to_next_timer(ex)) {
                continue;
            }
        }
    }
    rt_unlock(ex);
    return NULL;
}
