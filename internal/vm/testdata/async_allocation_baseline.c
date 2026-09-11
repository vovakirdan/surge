#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#include "rt_async_internal.h"

#include <errno.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

// This main replaces only rt_entry.o. The compiled Surge object and the native
// archive are the SAME objects used by the ordinary source build.
void __surge_start(void); // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
int rt_argc;
char** rt_argv_raw;
static rt_executor* observed_executor;
static void* volatile retained_channel;

static void observation_fail(const char* reason) {
    static const char prefix[] = "FAIL_ASYNC_ALLOCATION_CENSUS: ";
    (void)write(STDERR_FILENO, prefix, sizeof(prefix) - 1U);
    (void)write(STDERR_FILENO, reason, strlen(reason));
    (void)write(STDERR_FILENO, "\n", 1);
    _Exit(86);
}

static int allocation_baseline_tables_empty(const rt_executor* ex) {
    uint64_t next_task = atomic_load_explicit(&ex->next_id, memory_order_acquire);
    uint64_t next_scope = atomic_load_explicit(&ex->next_scope_id, memory_order_acquire);
    if (next_task == 0 || next_task >= RT_TASK_TABLE_SEGMENT_SIZE || next_scope == 0 ||
        next_scope >= RT_SCOPE_TABLE_SEGMENT_SIZE) {
        observation_fail("task/scope IDs exceeded the one-segment proof");
    }
    for (size_t i = 0; i < RT_TASK_TABLE_MAX_SEGMENTS; i++) {
        const rt_task_segment* segment =
            atomic_load_explicit(&ex->tasks_table.segments[i], memory_order_acquire);
        if ((i == 0) != (segment != NULL)) {
            observation_fail("task segment census differs from the prewarmed segment");
        }
        if (segment != NULL) {
            for (size_t slot = 0; slot < RT_TASK_TABLE_SEGMENT_SIZE; slot++) {
                if (atomic_load_explicit(&segment->slots[slot], memory_order_acquire) != NULL) {
                    return 0;
                }
            }
        }
    }
    for (size_t i = 0; i < RT_SCOPE_TABLE_MAX_SEGMENTS; i++) {
        const rt_scope_segment* segment =
            atomic_load_explicit(&ex->scopes_table.segments[i], memory_order_acquire);
        if ((i == 0) != (segment != NULL)) {
            observation_fail("scope segment census differs from the prewarmed segment");
        }
        if (segment != NULL) {
            for (size_t slot = 0; slot < RT_SCOPE_TABLE_SEGMENT_SIZE; slot++) {
                if (atomic_load_explicit(&segment->slots[slot], memory_order_acquire) != NULL) {
                    return 0;
                }
            }
        }
    }
    return 1;
}

static int allocation_baseline_empty(const rt_executor* ex, const rt_shard* shard) {
    const rt_scheduler* scheduler = &shard->scheduler;
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&ex->blocking_submitted, memory_order_acquire) != 0) {
        observation_fail("shutdown or blocking work is outside this proof");
    }
    if (scheduler->running_count != 0 || scheduler->publishing_count != 0 ||
        scheduler->inject.len != 0 || ex->control_waiters.len != 0 ||
        shard->waiter_store.len != 0 || shard->sleep_store.len != 0 ||
        shard->transport.data_len != 0 || shard->transport.control_len != 0 ||
        shard->transport.reply_reserved != 0 ||
        atomic_load_explicit(&ex->done_waiters, memory_order_acquire) != 0 ||
        atomic_load_explicit(&ex->blocking_running, memory_order_acquire) != 0) {
        return 0;
    }
    if (shard->waiter_store.cap != 16 || shard->waiter_store.entries == NULL ||
        scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        observation_fail("prewarmed waiter/scheduler capacity changed");
    }
    for (uint32_t i = 0; i < scheduler->worker_count; i++) {
        if (scheduler->local_queues[i].len != 0) {
            return 0;
        }
    }
    return allocation_baseline_tables_empty(ex);
}

static void allocation_baseline_observe(void) {
    rt_executor* ex = observed_executor;
    if (rt_current_task_id() != 0 || rt_lane_holds_control() || rt_lane_holds_any_shard() ||
        rt_lane_holds_token_lock() || ex == NULL || ex->runtime->shard_count != 1) {
        observation_fail("observer needs task-free, unlocked, single-shard main lane");
    }
    rt_shard* shard = &ex->runtime->shards[0];
    struct timespec began;
    if (clock_gettime(CLOCK_MONOTONIC, &began) != 0) {
        observation_fail("cannot start the observation deadline");
    }
    for (;;) {
        // Raw locks are deliberate: rt_*_unlock drains deferred reclamation.
        // An observer must never repair the lifetime it is trying to measure.
        // The production ordering is still control -> sole owner shard.
        int control_status = pthread_mutex_trylock(&ex->lock);
        if (control_status != 0 && control_status != EBUSY) {
            observation_fail("control trylock failed");
        }
        int empty = 0;
        if (control_status == 0) {
            int shard_status = pthread_mutex_trylock(&shard->lock);
            if (shard_status != 0 && shard_status != EBUSY) {
                observation_fail("shard trylock failed");
            }
            if (shard_status == 0) {
                empty = allocation_baseline_empty(ex, shard);
                if (pthread_mutex_unlock(&shard->lock) != 0) {
                    observation_fail("shard unlock failed");
                }
            }
            if (pthread_mutex_unlock(&ex->lock) != 0) {
                observation_fail("control unlock failed");
            }
        }
        if (empty) {
            static const char marker[] =
                "ASYNC_ALLOCATION_CENSUS: tasks=0 scopes=0 ready=0 running=0 publishing=0 "
                "waiters=0 sleeps=0 inbound=0 reserved=0 segments=1/1\n";
            if (write(STDOUT_FILENO, marker, sizeof(marker) - 1U) !=
                (ssize_t)(sizeof(marker) - 1U)) {
                observation_fail("census marker write failed");
            }
            return;
        }
        struct timespec now;
        if (clock_gettime(CLOCK_MONOTONIC, &now) != 0 || now.tv_sec - began.tv_sec >= 10) {
            observation_fail("quiescence deadline: live task/scope/queue/waiter state remains");
        }
        struct timespec pause = {.tv_sec = 0, .tv_nsec = 1000000};
        if (nanosleep(&pause, NULL) != 0 && errno != EINTR) {
            observation_fail("observation pause failed");
        }
    }
}

static void allocation_baseline_bootstrap(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || ex->runtime == NULL || ex->runtime->shard_count != 1) {
        observation_fail("bootstrap requires exactly one shard");
    }
    rt_control_lock(ex);
    ensure_task_cap(ex, 1);
    ensure_scope_cap(ex, 1);
    rt_shard* shard = &ex->runtime->shards[0];
    rt_shard_lock(shard);
    rt_runtime_status status = rt_waiter_store_ensure_cap(&shard->waiter_store);
    rt_shard_unlock(shard);
    rt_control_unlock(ex);
    if (status != RT_RUNTIME_STATUS_OK) {
        observation_fail("bootstrap waiter capacity failed");
    }
    observed_executor = ex;
    if (atexit(allocation_baseline_observe) != 0) {
        observation_fail("cannot register the exit observer");
    }
}

static void retained_word_move(void* destination, void* source) {
    *(uint32_t*)destination = *(uint32_t*)source;
    *(uint32_t*)source = 0;
}

static rt_carrier_status
retained_word_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

int main(int argc, char** argv) {
    allocation_baseline_bootstrap();
    if (argc != 3) {
        observation_fail("expected control/subject/retained-channel and workload count");
    }
    // Keep the usual argv[0]; remove only this C driver's mode argument.
    const char* mode = argv[1];
    argv[1] = argv[2];
    argv[2] = NULL;
    rt_argc = 2;
    rt_argv_raw = argv;
    if (strcmp(mode, "control") == 0) {
        return 0;
    }
    if (strcmp(mode, "retained-channel") == 0) {
        static const rt_value_ops operations = {
            .layout = {.size = sizeof(uint32_t),
                       .align = _Alignof(uint32_t),
                       .stride = sizeof(uint32_t)},
            .move_init = retained_word_move,
            .plan_cross = retained_word_plan,
        };
        // A real runtime resource, held outside task/scope tables on purpose.
        // The logical census must pass; the exact allocation delta must fail.
        retained_channel = rt_channel_new(1, &operations, 0);
        if (retained_channel == NULL) {
            observation_fail("negative-control channel allocation failed");
        }
    } else if (strcmp(mode, "subject") != 0) {
        observation_fail("unknown driver mode");
    }
    __surge_start();
    return 0;
}
