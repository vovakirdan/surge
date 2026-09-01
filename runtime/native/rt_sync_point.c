// Runtime V2 proving spike: test-only deterministic interleaving hooks.
//
// The entire translation unit is empty unless RT_TEST_SYNC_POINTS is defined,
// so a shipping build links no rendezvous code and exports no rt_sync_point_*
// symbol (enforced by check_sync_points.sh). See rt_sync_point.h for the
// ownership model and the allowlist.
//
// clock_gettime is POSIX, not C11: the harness build sees it through -pthread,
// the strict changed-file scan (no -pthread) does not, so ask for it by name
// the way rt_alloc.c and rt_async_state.c already do.
#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_sync_point.h"

#ifdef RT_TEST_SYNC_POINTS

#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// Arming action for a point, parsed once from SURGE_SYNC_POINT.
//   SURGE_SYNC_POINT="NAME:ACTION[,NAME:ACTION...]"
// ACTION is one of:
//   barrier  reaching thread waits at the shared, generation-reusable barrier
//            whose width is the number of `barrier` points armed; all barrier
//            participants are released together (simultaneous entry into the
//            racy region, for the StoreLoad litmus).
//   block    reaching thread waits for one permit from the shared semaphore
//            (an ordered interleaving: hold a thread at this window).
//   open     reaching thread grants one permit, then continues (releases a
//            thread blocked at its `block` window).
typedef enum rt_sp_action {
    RT_SP_ACTION_NONE = 0,
    RT_SP_ACTION_BARRIER,
    RT_SP_ACTION_BLOCK,
    RT_SP_ACTION_OPEN
} rt_sp_action;

// A generation barrier (portable stand-in for pthread_barrier_t, which glibc
// hides under -std=c11): reusable across iterations, sense-reversed by
// generation so a thread cannot skip the next rendezvous.
typedef struct rt_sp_barrier {
    pthread_mutex_t mtx;
    pthread_cond_t cond;
    unsigned needed;
    unsigned arrived;
    unsigned generation;
} rt_sp_barrier;

// A counting semaphore (block/open turnstile), re-armable across iterations.
typedef struct rt_sp_sem {
    pthread_mutex_t mtx;
    pthread_cond_t cond;
    unsigned permits;
} rt_sp_sem;

static pthread_once_t rt_sp_once = PTHREAD_ONCE_INIT;
static rt_sp_action rt_sp_armed[RT_SYNC_POINT_COUNT];
static _Atomic unsigned rt_sp_reached[RT_SYNC_POINT_COUNT];
static rt_sp_barrier rt_sp_barrier_state = {
    PTHREAD_MUTEX_INITIALIZER, PTHREAD_COND_INITIALIZER, 0, 0, 0};
static rt_sp_sem rt_sp_sem_state = {PTHREAD_MUTEX_INITIALIZER, PTHREAD_COND_INITIALIZER, 0};
static pthread_mutex_t rt_sp_reached_mtx = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t rt_sp_reached_cond = PTHREAD_COND_INITIALIZER;

// Bounded so a mis-armed test fails loud instead of hanging the harness.
#define RT_SP_TIMEOUT_SECS 10

// The name every sync point answers to. This is the only way a stand can reach a
// hook: SURGE_SYNC_POINT carries names, and rt_sp_arm below walks this table to
// turn one into an id, aborting the process when no row matches. So an
// enumerator with no row here is a declared hook that nothing can ever arm, and
// the abort lands on the stand rather than on whoever forgot the row. Every
// enumerator in rt_sync_point.h must have a row, and each row must return its
// own case's spelling; check_sync_points.sh refuses both departures.
static const char* rt_sp_name(rt_sync_point_id id) {
    switch (id) {
        case RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE:
            return "SP_CANCEL_BEFORE_WAKE";
        case RT_SYNC_POINT_SP_PARK_BEFORE_WAITING:
            return "SP_PARK_BEFORE_WAITING";
        case RT_SYNC_POINT_SP_PARK_AFTER_INITIAL_TOKEN_CHECK:
            return "SP_PARK_AFTER_INITIAL_TOKEN_CHECK";
        case RT_SYNC_POINT_SP_PARK_ABORT_AFTER_REQUEUE:
            return "SP_PARK_ABORT_AFTER_REQUEUE";
        case RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD:
            return "SP_MARKDONE_BEFORE_DONEWAITERS_LOAD";
        case RT_SYNC_POINT_SP_AWAIT_AFTER_INCREMENT:
            return "SP_AWAIT_AFTER_INCREMENT";
        case RT_SYNC_POINT_SP_AWAIT_BEFORE_DONECV_WAIT:
            return "SP_AWAIT_BEFORE_DONECV_WAIT";
        case RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER:
            return "SP_TASK_POLL_AFTER_JOIN_REGISTER";
        case RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER:
            return "SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER";
        case RT_SYNC_POINT_SP_WAKEKEY_MID_DRAIN:
            return "SP_WAKEKEY_MID_DRAIN";
        case RT_SYNC_POINT_SP_MIGRATE_GAP:
            return "SP_MIGRATE_GAP";
        case RT_SYNC_POINT_SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK:
            return "SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK";
        case RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK:
            return "SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK";
        case RT_SYNC_POINT_SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD:
            return "SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD";
        case RT_SYNC_POINT_SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE:
            return "SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE";
        case RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND:
            return "SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND";
        case RT_SYNC_POINT_SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE:
            return "SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE";
        case RT_SYNC_POINT_SP_REMOTE_TASK_BEFORE_OWNER_REGISTER:
            return "SP_REMOTE_TASK_BEFORE_OWNER_REGISTER";
        case RT_SYNC_POINT_SP_REMOTE_TASK_AFTER_OWNER_REGISTER:
            return "SP_REMOTE_TASK_AFTER_OWNER_REGISTER";
        case RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH:
            return "SP_REMOTE_SPAWN_BEFORE_DISPATCH";
        case RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH:
            return "SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH";
        case RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK:
            return "SP_REMOTE_SPAWN_BEFORE_ACK";
        case RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH:
            return "SP_IMMEDIATE_ON_BEFORE_DISPATCH";
        case RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH:
            return "SP_IMMEDIATE_ON_BEFORE_PUBLISH";
        case RT_SYNC_POINT_SP_IMMEDIATE_ON_AFTER_PUBLISH:
            return "SP_IMMEDIATE_ON_AFTER_PUBLISH";
        case RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK:
            return "SP_READY_REQUEUE_BEFORE_LOCK";
        case RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL:
            return "SP_WAKE_BEFORE_STALE_REMOVAL";
        case RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY:
            return "SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY";
        case RT_SYNC_POINT_SP_FAR_SELECT_BEFORE_DISPATCH:
            return "SP_FAR_SELECT_BEFORE_DISPATCH";
        case RT_SYNC_POINT_SP_CARRIER_JUMBO_ADMITTED:
            return "SP_CARRIER_JUMBO_ADMITTED";
        case RT_SYNC_POINT_SP_TRANSPORT_DATA_SLOT_TASK_PARKED:
            return "SP_TRANSPORT_DATA_SLOT_TASK_PARKED";
        case RT_SYNC_POINT_SP_SLEEP_FIRED_BEFORE_WAKE:
            return "SP_SLEEP_FIRED_BEFORE_WAKE";
        case RT_SYNC_POINT_SP_CHANNEL_LAST_RELEASE_BEFORE_FREE:
            return "SP_CHANNEL_LAST_RELEASE_BEFORE_FREE";
        case RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY:
            return "SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY";
        case RT_SYNC_POINT_SP_SCOPE_TEARDOWN_BEFORE_REGISTER:
            return "SP_SCOPE_TEARDOWN_BEFORE_REGISTER";
        case RT_SYNC_POINT_SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT:
            return "SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT";
        case RT_SYNC_POINT_SP_MARKDONE_AFTER_SEAL_BEFORE_DONE:
            return "SP_MARKDONE_AFTER_SEAL_BEFORE_DONE";
        case RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS:
            return "SP_BLOCKING_POP_BEFORE_STATUS";
        case RT_SYNC_POINT_SP_BLOCKING_STATE_BEFORE_BODY:
            return "SP_BLOCKING_STATE_BEFORE_BODY";
        case RT_SYNC_POINT_SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN:
            return "SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN";
        case RT_SYNC_POINT_SP_INLINE_CHILD_TAKEN_OFF_QUEUE:
            return "SP_INLINE_CHILD_TAKEN_OFF_QUEUE";
        case RT_SYNC_POINT_SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH:
            return "SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH";
        case RT_SYNC_POINT_SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE:
            return "SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE";
        case RT_SYNC_POINT_SP_CLONE_READER_OUT_OF_LOCK:
            return "SP_CLONE_READER_OUT_OF_LOCK";
        case RT_SYNC_POINT_SP_CANCEL_AT_COMMITTED_RESULT:
            return "SP_CANCEL_AT_COMMITTED_RESULT";
        case RT_SYNC_POINT_SP_RESULT_CAPABILITY_BEFORE_MATCH:
            return "SP_RESULT_CAPABILITY_BEFORE_MATCH";
        default:
            return "";
    }
}

static rt_sp_action rt_sp_parse_action(const char* s, size_t len) {
    if (len == 7 && strncmp(s, "barrier", 7) == 0) {
        return RT_SP_ACTION_BARRIER;
    }
    if (len == 5 && strncmp(s, "block", 5) == 0) {
        return RT_SP_ACTION_BLOCK;
    }
    if (len == 4 && strncmp(s, "open", 4) == 0) {
        return RT_SP_ACTION_OPEN;
    }
    fprintf(stderr, "rt_sync_point: unknown action '%.*s'\n", (int)len, s);
    abort();
}

static void rt_sp_arm(const char* name, size_t name_len, rt_sp_action action) {
    for (int id = 1; id < RT_SYNC_POINT_COUNT; id++) {
        const char* known = rt_sp_name((rt_sync_point_id)id);
        if (strlen(known) == name_len && strncmp(known, name, name_len) == 0) {
            rt_sp_armed[id] = action;
            if (action == RT_SP_ACTION_BARRIER) {
                rt_sp_barrier_state.needed++;
            }
            return;
        }
    }
    fprintf(stderr, "rt_sync_point: unknown point '%.*s'\n", (int)name_len, name);
    abort();
}

static void rt_sp_init(void) {
    const char* spec = getenv("SURGE_SYNC_POINT");
    if (spec == NULL || spec[0] == '\0') {
        return;
    }
    const char* p = spec;
    while (*p != '\0') {
        const char* entry_end = strchr(p, ',');
        size_t entry_len = (entry_end != NULL) ? (size_t)(entry_end - p) : strlen(p);
        const char* colon = memchr(p, ':', entry_len);
        if (colon == NULL) {
            fprintf(stderr, "rt_sync_point: entry '%.*s' is not NAME:ACTION\n", (int)entry_len, p);
            abort();
        }
        size_t name_len = (size_t)(colon - p);
        const char* action_str = colon + 1;
        size_t action_len = entry_len - name_len - 1;
        rt_sp_arm(p, name_len, rt_sp_parse_action(action_str, action_len));
        if (entry_end == NULL) {
            break;
        }
        p = entry_end + 1;
    }
}

static void rt_sp_deadline(struct timespec* ts) {
    clock_gettime(CLOCK_REALTIME, ts);
    ts->tv_sec += RT_SP_TIMEOUT_SECS;
}

static void rt_sp_barrier_wait(void) {
    struct timespec ts;
    rt_sp_deadline(&ts);
    pthread_mutex_lock(&rt_sp_barrier_state.mtx);
    unsigned gen = rt_sp_barrier_state.generation;
    if (++rt_sp_barrier_state.arrived >= rt_sp_barrier_state.needed) {
        rt_sp_barrier_state.arrived = 0;
        rt_sp_barrier_state.generation++;
        pthread_cond_broadcast(&rt_sp_barrier_state.cond);
    } else {
        while (gen == rt_sp_barrier_state.generation) {
            if (pthread_cond_timedwait(&rt_sp_barrier_state.cond, &rt_sp_barrier_state.mtx, &ts) !=
                0) {
                pthread_mutex_unlock(&rt_sp_barrier_state.mtx);
                fprintf(stderr, "rt_sync_point: barrier timed out (deadlocked arming?)\n");
                abort();
            }
        }
    }
    pthread_mutex_unlock(&rt_sp_barrier_state.mtx);
}

static void rt_sp_sem_block(void) {
    struct timespec ts;
    rt_sp_deadline(&ts);
    pthread_mutex_lock(&rt_sp_sem_state.mtx);
    while (rt_sp_sem_state.permits == 0) {
        if (pthread_cond_timedwait(&rt_sp_sem_state.cond, &rt_sp_sem_state.mtx, &ts) != 0) {
            pthread_mutex_unlock(&rt_sp_sem_state.mtx);
            fprintf(stderr, "rt_sync_point: block timed out (no matching open?)\n");
            abort();
        }
    }
    rt_sp_sem_state.permits--;
    pthread_mutex_unlock(&rt_sp_sem_state.mtx);
}

static void rt_sp_sem_open(void) {
    pthread_mutex_lock(&rt_sp_sem_state.mtx);
    rt_sp_sem_state.permits++;
    pthread_cond_signal(&rt_sp_sem_state.cond);
    pthread_mutex_unlock(&rt_sp_sem_state.mtx);
}

void rt_sync_point_reach(rt_sync_point_id id) {
    if (id <= RT_SYNC_POINT_NONE || id >= RT_SYNC_POINT_COUNT) {
        return;
    }
    pthread_once(&rt_sp_once, rt_sp_init);
    pthread_mutex_lock(&rt_sp_reached_mtx);
    atomic_fetch_add_explicit(&rt_sp_reached[id], 1u, memory_order_relaxed);
    pthread_cond_broadcast(&rt_sp_reached_cond);
    pthread_mutex_unlock(&rt_sp_reached_mtx);
    switch (rt_sp_armed[id]) {
        case RT_SP_ACTION_BARRIER:
            rt_sp_barrier_wait();
            break;
        case RT_SP_ACTION_BLOCK:
            rt_sp_sem_block();
            break;
        case RT_SP_ACTION_OPEN:
            rt_sp_sem_open();
            break;
        case RT_SP_ACTION_NONE:
        default:
            break;
    }
}

// Test accessor: how many times a point was reached this process. Lets a proof
// assert the window was actually exercised (never a silent skip).
unsigned rt_sync_point_reached_count(rt_sync_point_id id) {
    if (id <= RT_SYNC_POINT_NONE || id >= RT_SYNC_POINT_COUNT) {
        return 0;
    }
    return atomic_load_explicit(&rt_sp_reached[id], memory_order_relaxed);
}

// Driver-side wait for a runtime thread to cross a point. This is a test-only
// condition-variable rendezvous, not a sleep/poll loop; the timeout is only a
// deadlock guard and never a success oracle.
int rt_sync_point_wait_until_after(rt_sync_point_id id, unsigned before) {
    if (id <= RT_SYNC_POINT_NONE || id >= RT_SYNC_POINT_COUNT) {
        return 0;
    }
    struct timespec ts;
    rt_sp_deadline(&ts);
    pthread_mutex_lock(&rt_sp_reached_mtx);
    while (atomic_load_explicit(&rt_sp_reached[id], memory_order_relaxed) <= before) {
        if (pthread_cond_timedwait(&rt_sp_reached_cond, &rt_sp_reached_mtx, &ts) != 0) {
            pthread_mutex_unlock(&rt_sp_reached_mtx);
            return 0;
        }
    }
    pthread_mutex_unlock(&rt_sp_reached_mtx);
    return 1;
}

// Driver-callable release: grant one permit so one thread blocked at a `block`
// point resumes. Used by a test driver to order an interleaving explicitly
// (hold the target at its window, perform the racing action, then release it).
void rt_sync_point_open(void) {
    rt_sp_sem_open();
}

#else

// Keep the translation unit non-empty (ISO C forbids an empty TU) without
// emitting any symbol, so the release build stays hook-free.
typedef int rt_sync_point_translation_unit_not_empty;

#endif // RT_TEST_SYNC_POINTS
