#include "rt_async_internal.h"

#include <errno.h>
#include <limits.h>
#include <signal.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

// Async runtime state, queues, and memory helpers.
//
// MT NOTES (iteration 1):
// - ST executor stored tasks in exec_state.tasks and scheduled via the global injection queue.
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
_Thread_local int tls_worker_id = -1;
static pthread_once_t exec_once = PTHREAD_ONCE_INIT;

struct rt_worker_ctx {
    rt_executor* ex;
    uint32_t worker_id;
    uint64_t sched_rng;
};

enum {
    SCHED_SRC_LOCAL = 0,
    SCHED_SRC_INJECT = 1,
    SCHED_SRC_STEAL = 2,
};

static volatile sig_atomic_t trace_exec_enabled_flag = 0;
static volatile sig_atomic_t trace_sched_enabled_flag = 0;
static _Atomic uint64_t trace_wake_called_total;
static _Atomic uint64_t trace_wake_enqueued_total;
static _Atomic uint64_t trace_wake_ignored_completed_total;
static _Atomic uint64_t trace_park_attempt_total;
static _Atomic uint64_t trace_park_committed_total;
static _Atomic uint64_t trace_worker_sleep_total;
static _Atomic uint64_t trace_worker_wake_total;
static uint64_t trace_sched_hash;
static uint64_t trace_sched_events;
static uint64_t trace_sched_local_pops;
static uint64_t trace_sched_inject_pops;
static uint64_t trace_sched_steal_pops;

static int async_debug_enabled_cached = -1;

int rt_async_debug_enabled(void) {
    if (async_debug_enabled_cached >= 0) {
        return async_debug_enabled_cached;
    }
    const char* value = getenv("SURGE_ASYNC_DEBUG");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        async_debug_enabled_cached = 0;
        return 0;
    }
    async_debug_enabled_cached = 1;
    return 1;
}

void rt_async_debug_printf(const char* fmt, ...) {
    if (!rt_async_debug_enabled() || fmt == NULL) {
        return;
    }
    char buf[512];
    va_list args;
    va_start(args, fmt);
#if defined(__clang__)
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wformat-nonliteral"
#elif defined(__GNUC__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wformat-nonliteral"
#endif
    int n = vsnprintf(buf, sizeof(buf), fmt, args);
#if defined(__clang__)
#pragma clang diagnostic pop
#elif defined(__GNUC__)
#pragma GCC diagnostic pop
#endif
    va_end(args);
    if (n <= 0) {
        return;
    }
    uint64_t len = (uint64_t)n;
    if ((size_t)n >= sizeof(buf)) {
        len = (uint64_t)(sizeof(buf) - 1);
    }
    rt_write_stderr((const uint8_t*)buf, len);
}

static int trace_exec_enabled(void) {
    return trace_exec_enabled_flag != 0;
}

static int trace_sched_enabled(void) {
    return trace_sched_enabled_flag != 0;
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
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " blocking_submitted=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_submitted, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " blocking_running=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_running, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " blocking_completed=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_completed, memory_order_relaxed));
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " blocking_cancel_requested=");
    pos = trace_exec_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_cancel_requested, memory_order_relaxed));
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}

static void trace_sched_record(uint8_t source, uint64_t id) {
    if (!trace_sched_enabled()) {
        return;
    }
    trace_sched_events++;
    if (source == 0) {
        trace_sched_local_pops++;
    } else if (source == 1) {
        trace_sched_inject_pops++;
    } else if (source == 2) {
        trace_sched_steal_pops++;
    }
    uint64_t mix = id ^ ((uint64_t)source << 56);
    trace_sched_hash ^= mix;
    trace_sched_hash *= UINT64_C(1099511628211);
}

static void trace_exec_signal_handler(int sig) {
    (void)sig;
    trace_exec_dump("sigusr1");
}

static void trace_sched_init(void) {
    const char* value = getenv("SURGE_SCHED_TRACE");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return;
    }
    trace_sched_enabled_flag = 1;
    trace_sched_hash = UINT64_C(1469598103934665603);
    trace_sched_events = 0;
    trace_sched_local_pops = 0;
    trace_sched_inject_pops = 0;
    trace_sched_steal_pops = 0;
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

waker_key blocking_key(uint64_t id) {
    waker_key key = {WAKER_BLOCKING, id};
    return key;
}

waker_key channel_send_key(const rt_channel* ch) {
    waker_key key = {WAKER_CHAN_SEND, (uint64_t)(uintptr_t)ch};
    return key;
}

waker_key channel_recv_key(const rt_channel* ch) {
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

static uint32_t rt_env_blocking_count(void) {
    const char* value = getenv("SURGE_BLOCKING_THREADS");
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

// Seeded scheduler mode provides deterministic scheduler choices given the same seed and the same
// arrival order of external events; it does not control I/O timing or OS thread interleavings.
static uint8_t rt_env_sched_mode(void) {
    const char* value = getenv("SURGE_SCHED");
    if (value == NULL || value[0] == '\0') {
        return SCHED_PARALLEL;
    }
    if (strcmp(value, "seeded") == 0) {
        return SCHED_SEEDED;
    }
    if (strcmp(value, "parallel") == 0) {
        return SCHED_PARALLEL;
    }
    return SCHED_PARALLEL;
}

static uint64_t rt_env_sched_seed(void) {
    const char* value = getenv("SURGE_SCHED_SEED");
    if (value == NULL || value[0] == '\0') {
        return 0;
    }
    errno = 0;
    char* end = NULL;
    unsigned long long parsed = strtoull(value, &end, 0);
    if (end == value || errno != 0) {
        return 0;
    }
    return (uint64_t)parsed;
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

static uint32_t rt_default_blocking_count(uint32_t workers) {
    if (workers < 1) {
        workers = 1;
    }
    return workers;
}

static void rt_start_workers(rt_executor* ex);
static void* rt_worker_main(void* arg);
static void* rt_io_main(void* arg);
static void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome);
static int runnable_is_empty(const rt_executor* ex);
static int worker_next_ready(rt_executor* ex, uint32_t worker_id, uint64_t* out_id);
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
    trace_sched_init();
    uint32_t threads = rt_env_worker_count();
    if (threads == 0) {
        threads = rt_default_worker_count();
    }
    ex->worker_count = threads;
    ex->sched_mode = rt_env_sched_mode();
    ex->sched_seed = rt_env_sched_seed();
    uint32_t blocking_threads = rt_env_blocking_count();
    if (blocking_threads == 0) {
        blocking_threads = rt_default_blocking_count(ex->worker_count);
    }
    ex->blocking_count = blocking_threads;
    if (ex->worker_count > 0) {
        ex->local_queues = (rt_deque*)rt_alloc(
            (uint64_t)ex->worker_count * (uint64_t)sizeof(rt_deque), _Alignof(rt_deque));
        if (ex->local_queues == NULL) {
            panic_msg("async: local queue allocation failed");
        } else {
            memset(ex->local_queues, 0, ex->worker_count * sizeof(rt_deque));
        }
    }
    if (ex->worker_count > 1) {
        rt_start_workers(ex);
    }
    rt_blocking_init(ex);
    ex->initialized = 1;
}

rt_executor* ensure_exec(void) {
    pthread_once(&exec_once, exec_init_once);
    return &exec_state;
}

uint64_t rt_worker_count(void) {
    const rt_executor* ex = ensure_exec();
    return (uint64_t)ex->worker_count;
}

static int runnable_is_empty(const rt_executor* ex) {
    if (ex == NULL) {
        return 1;
    }
    if (ex->inject.len > 0) {
        return 0;
    }
    if (ex->local_queues == NULL || ex->worker_count == 0) {
        return 1;
    }
    for (uint32_t i = 0; i < ex->worker_count; i++) {
        if (ex->local_queues[i].len > 0) {
            return 0;
        }
    }
    return 1;
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

void rt_sched_trace_dump(void) {
    if (!trace_sched_enabled()) {
        return;
    }
    if (!exec_state.initialized) {
        return;
    }
    rt_lock(&exec_state);
    uint64_t local = trace_sched_local_pops;
    uint64_t inject = trace_sched_inject_pops;
    uint64_t steal = trace_sched_steal_pops;
    uint64_t events = trace_sched_events;
    uint64_t hash = trace_sched_hash;
    uint64_t seed = exec_state.sched_seed;
    uint8_t mode = exec_state.sched_mode;
    rt_unlock(&exec_state);

    char buf[256];
    size_t pos = 0;
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), "SCHED_TRACE mode=");
    pos = trace_exec_append_literal(
        buf, pos, sizeof(buf), mode == SCHED_SEEDED ? "seeded" : "parallel");
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " seed=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), seed);
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " local=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), local);
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " inject=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), inject);
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " steal=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), steal);
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " events=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), events);
    pos = trace_exec_append_literal(buf, pos, sizeof(buf), " hash=");
    pos = trace_exec_append_u64(buf, pos, sizeof(buf), hash);
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}

static uint64_t sched_next_u64(rt_worker_ctx* ctx) {
    if (ctx == NULL) {
        return 0;
    }
    uint64_t z = (ctx->sched_rng += UINT64_C(0x9e3779b97f4a7c15));
    z = (z ^ (z >> 30)) * UINT64_C(0xbf58476d1ce4e5b9);
    z = (z ^ (z >> 27)) * UINT64_C(0x94d049bb133111eb);
    return z ^ (z >> 31);
}

static void rt_start_workers(rt_executor* ex) {
    if (ex == NULL || ex->worker_count <= 1) {
        return;
    }
    uint32_t count = ex->worker_count;
    size_t total = (size_t)count + 1;
    pthread_t* threads =
        (pthread_t*)rt_alloc((uint64_t)total * (uint64_t)sizeof(pthread_t), _Alignof(pthread_t));
    if (threads == NULL) {
        panic_msg("async: worker allocation failed");
        return;
    }
    rt_worker_ctx* ctxs = (rt_worker_ctx*)rt_alloc(
        (uint64_t)count * (uint64_t)sizeof(rt_worker_ctx), _Alignof(rt_worker_ctx));
    if (ctxs == NULL) {
        panic_msg("async: worker context allocation failed");
        return;
    }
    memset(ctxs, 0, count * sizeof(rt_worker_ctx));
    ex->workers = threads;
    ex->worker_ctxs = ctxs;
    if (pthread_create(&threads[0], NULL, rt_io_main, ex) != 0) {
        panic_msg("async: io worker start failed");
        return;
    }
    (void)pthread_detach(threads[0]);
    ex->io_started = 1;
    for (uint32_t i = 0; i < count; i++) {
        ctxs[i].ex = ex;
        ctxs[i].worker_id = i;
        ctxs[i].sched_rng = ex->sched_seed + UINT64_C(0x9e3779b97f4a7c15) * (uint64_t)(i + 1);
        if (pthread_create(&threads[i + 1], NULL, rt_worker_main, &ctxs[i]) != 0) {
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

static int
deque_reserve(rt_deque* dq, size_t want, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (want <= dq->cap) {
        return 1;
    }
    size_t next_cap = dq->cap == 0 ? 16 : dq->cap;
    while (next_cap < want) {
        if (next_cap > SIZE_MAX / 2) {
            panic_msg(overflow_msg);
            return 0;
        }
        next_cap *= 2;
    }
    if (next_cap > SIZE_MAX / sizeof(uint64_t)) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t old_size = dq->cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    if (old_size > UINT64_MAX || new_size > UINT64_MAX) {
        panic_msg(overflow_msg);
        return 0;
    }
    uint64_t* next = (uint64_t*)rt_alloc((uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg(alloc_msg);
        return 0;
    }
    if (dq->len > 0 && dq->buf != NULL) {
        memcpy(next, dq->buf + dq->head, dq->len * sizeof(uint64_t));
    }
    if (dq->buf != NULL && dq->cap > 0) {
        rt_free((uint8_t*)dq->buf, (uint64_t)old_size, _Alignof(uint64_t));
    }
    dq->buf = next;
    dq->cap = next_cap;
    dq->head = 0;
    return 1;
}

static int
deque_ensure_space(rt_deque* dq, size_t extra, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (dq->head > SIZE_MAX - dq->len) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t used = dq->head + dq->len;
    if (extra > SIZE_MAX - used) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t want = used + extra;
    if (want <= dq->cap) {
        return 1;
    }
    if (dq->head > 0 && dq->len > 0 && dq->buf != NULL) {
        memmove(dq->buf, dq->buf + dq->head, dq->len * sizeof(uint64_t));
        dq->head = 0;
        used = dq->len;
        if (extra > SIZE_MAX - used) {
            panic_msg(overflow_msg);
            return 0;
        }
        want = used + extra;
        if (want <= dq->cap) {
            return 1;
        }
    }
    return deque_reserve(dq, want, overflow_msg, alloc_msg);
}

static int
deque_push_tail(rt_deque* dq, uint64_t id, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (!deque_ensure_space(dq, 1, overflow_msg, alloc_msg)) {
        return 0;
    }
    dq->buf[dq->head + dq->len] = id;
    dq->len++;
    return 1;
}

static int deque_pop_head(rt_deque* dq, uint64_t* out_id) {
    if (dq == NULL || dq->len == 0) {
        return 0;
    }
    uint64_t id = dq->buf[dq->head];
    dq->head++;
    dq->len--;
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (out_id != NULL) {
        *out_id = id;
    }
    return 1;
}

static int deque_pop_tail(rt_deque* dq, uint64_t* out_id) {
    if (dq == NULL || dq->len == 0) {
        return 0;
    }
    size_t idx = dq->head + dq->len - 1;
    uint64_t id = dq->buf[idx];
    dq->len--;
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (out_id != NULL) {
        *out_id = id;
    }
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

static rt_deque* current_local_queue(rt_executor* ex) {
    if (ex == NULL || ex->local_queues == NULL || ex->worker_count == 0) {
        return NULL;
    }
    if (tls_worker_id < 0 || (uint32_t)tls_worker_id >= ex->worker_count) {
        return NULL;
    }
    return &ex->local_queues[(uint32_t)tls_worker_id];
}

static int
pop_task_from_deque(rt_executor* ex, rt_deque* dq, int lifo, uint64_t* out_id, uint8_t source) {
    if (ex == NULL || dq == NULL) {
        return 0;
    }
    while (dq->len > 0) {
        uint64_t id = 0;
        if (lifo) {
            if (!deque_pop_tail(dq, &id)) {
                return 0;
            }
        } else {
            if (!deque_pop_head(dq, &id)) {
                return 0;
            }
        }
        rt_task* task = get_task(ex, id);
        uint8_t status = task_status_load(task);
        if (task == NULL || status == TASK_DONE || status == TASK_RUNNING) {
            if (task != NULL) {
                // Clear stale enqueue flags for discarded entries (e.g., duplicates).
                task_enqueued_store(task, 0);
            }
            continue;
        }
        task_enqueued_store(task, 0);
        trace_sched_record(source, id);
        if (out_id != NULL) {
            *out_id = id;
        }
        return 1;
    }
    return 0;
}

static int ready_push_inner(rt_executor* ex, uint64_t id, int force_inject) {
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
    // Injection policy:
    // - Worker thread: enqueue locally (LIFO pop) to keep cache locality.
    // - Non-worker thread (main/I/O/external): enqueue on the global injection queue.
    // No last-worker affinity is tracked; wake/spawn follows the current thread.
    rt_deque* local = NULL;
    if (!force_inject) {
        local = current_local_queue(ex);
    }
    if (local != NULL) {
        if (!deque_push_tail(
                local, id, "async: local queue overflow", "async: local queue allocation failed")) {
            return 0;
        }
    } else {
        if (!deque_push_tail(&ex->inject,
                             id,
                             "async: inject queue overflow",
                             "async: inject queue allocation failed")) {
            return 0;
        }
    }
    task_enqueued_store(task, 1);
    task_status_store(task, TASK_READY);
    pthread_cond_signal(&ex->ready_cv);
    return 1;
}

void ready_push(rt_executor* ex, uint64_t id) {
    (void)ready_push_inner(ex, id, 0);
}

int ready_pop(rt_executor* ex, uint64_t* out_id) {
    return pop_task_from_deque(ex, &ex->inject, 0, out_id, SCHED_SRC_INJECT);
}

static int worker_next_ready(rt_executor* ex, uint32_t worker_id, uint64_t* out_id) {
    if (ex == NULL) {
        return 0;
    }
    if (ex->sched_mode == SCHED_SEEDED) {
        rt_worker_ctx* ctx = ex->worker_ctxs != NULL && worker_id < ex->worker_count
                                 ? &ex->worker_ctxs[worker_id]
                                 : NULL;
        rt_deque* local = ex->local_queues != NULL && worker_id < ex->worker_count
                              ? &ex->local_queues[worker_id]
                              : NULL;
        int local_has = local != NULL && local->len > 0;
        int inject_has = ex->inject.len > 0;
        int others_have = 0;
        if (ex->local_queues != NULL && ex->worker_count > 1) {
            for (uint32_t i = 0; i < ex->worker_count; i++) {
                if (i == worker_id) {
                    continue;
                }
                if (ex->local_queues[i].len > 0) {
                    others_have = 1;
                    break;
                }
            }
        }
        if (local_has && inject_has) {
            if ((sched_next_u64(ctx) & 1U) == 0U) {
                if (pop_task_from_deque(ex, local, 1, out_id, SCHED_SRC_LOCAL)) {
                    return 1;
                }
                if (pop_task_from_deque(ex, &ex->inject, 0, out_id, SCHED_SRC_INJECT)) {
                    return 1;
                }
            } else {
                if (pop_task_from_deque(ex, &ex->inject, 0, out_id, SCHED_SRC_INJECT)) {
                    return 1;
                }
                if (pop_task_from_deque(ex, local, 1, out_id, SCHED_SRC_LOCAL)) {
                    return 1;
                }
            }
        } else if (local_has) {
            if (pop_task_from_deque(ex, local, 1, out_id, SCHED_SRC_LOCAL)) {
                return 1;
            }
        } else if (inject_has) {
            if (others_have && (sched_next_u64(ctx) & 1U) != 0U) {
                if (ex->worker_count > 1) {
                    uint32_t span = ex->worker_count - 1;
                    uint32_t start =
                        (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) % ex->worker_count;
                    for (uint32_t offset = 0; offset < span; offset++) {
                        uint32_t victim = start + offset;
                        if (victim >= ex->worker_count) {
                            victim -= ex->worker_count;
                        }
                        if (victim == worker_id) {
                            continue;
                        }
                        if (pop_task_from_deque(
                                ex, &ex->local_queues[victim], 0, out_id, SCHED_SRC_STEAL)) {
                            return 1;
                        }
                    }
                }
            }
            if (pop_task_from_deque(ex, &ex->inject, 0, out_id, SCHED_SRC_INJECT)) {
                return 1;
            }
        }
        if (ex->local_queues == NULL || ex->worker_count <= 1) {
            return 0;
        }
        uint32_t span = ex->worker_count - 1;
        uint32_t start =
            (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) % ex->worker_count;
        for (uint32_t offset = 0; offset < span; offset++) {
            uint32_t victim = start + offset;
            if (victim >= ex->worker_count) {
                victim -= ex->worker_count;
            }
            if (victim == worker_id) {
                continue;
            }
            if (pop_task_from_deque(ex, &ex->local_queues[victim], 0, out_id, SCHED_SRC_STEAL)) {
                return 1;
            }
        }
        return 0;
    }
    if (ex->local_queues != NULL && worker_id < ex->worker_count) {
        if (pop_task_from_deque(ex, &ex->local_queues[worker_id], 1, out_id, SCHED_SRC_LOCAL)) {
            return 1;
        }
    }
    if (pop_task_from_deque(ex, &ex->inject, 0, out_id, SCHED_SRC_INJECT)) {
        return 1;
    }
    if (ex->local_queues == NULL || ex->worker_count <= 1) {
        return 0;
    }
    for (uint32_t offset = 1; offset < ex->worker_count; offset++) {
        uint32_t victim = (worker_id + offset) % ex->worker_count;
        if (victim == worker_id) {
            continue;
        }
        if (pop_task_from_deque(ex, &ex->local_queues[victim], 0, out_id, SCHED_SRC_STEAL)) {
            return 1;
        }
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
    if (ready_push_inner(ex, id, 0)) {
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
                (uint64_t)task->wait_keys_cap * (uint64_t)sizeof(waker_key),
                _Alignof(waker_key));
    }
    if (task->select_timers != NULL && task->select_timers_cap > 0) {
        rt_free((uint8_t*)task->select_timers,
                (uint64_t)task->select_timers_cap * (uint64_t)sizeof(uint64_t),
                _Alignof(uint64_t));
    }
    if (task->children != NULL && task->children_cap > 0) {
        rt_free((uint8_t*)task->children,
                (uint64_t)task->children_cap * (uint64_t)sizeof(uint64_t),
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
    if (task->kind == TASK_KIND_BLOCKING) {
        rt_blocking_request_cancel(ex, task);
    }
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
            // Yielded tasks go through the inject queue to avoid local LIFO starvation.
            (void)ready_push_inner(ex, task->id, 1);
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
    rt_worker_ctx* ctx = (rt_worker_ctx*)arg;
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    uint32_t worker_id = ctx != NULL ? ctx->worker_id : 0;
    if (ex == NULL) {
        return NULL;
    }
    tls_worker_id = (int)worker_id;
    rt_set_current_task(NULL);
    for (;;) {
        rt_lock(ex);
        uint64_t id = 0;
        while (!ex->shutdown && !worker_next_ready(ex, worker_id, &id)) {
            trace_exec_inc(&trace_worker_sleep_total);
            pthread_cond_wait(&ex->ready_cv, &ex->lock);
            trace_exec_inc(&trace_worker_wake_total);
        }
        if (ex->shutdown) {
            rt_unlock(ex);
            break;
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
            if (ex->running_count == 0 && runnable_is_empty(ex)) {
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
        if (ex->running_count == 0 && runnable_is_empty(ex)) {
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
        int idle = ex->running_count == 0 && runnable_is_empty(ex);

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
