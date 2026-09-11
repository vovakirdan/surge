//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

func TestRuntimeV2ChannelSendOfferRepeatedParkedCancellation(t *testing.T) {
	bin := buildRuntimeV2LifecycleHarness(t)
	for _, workers := range []string{"1", "8"} {
		for _, mode := range []string{"parked", "late"} {
			t.Run("workers-"+workers+"/"+mode, func(t *testing.T) {
				late := "0"
				if mode == "late" {
					late = "1"
				}
				env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS="+workers,
					"SURGE_BLOCKING_THREADS=1", "SURGE_OFFER_CANCEL_LATE="+late)
				stdout, stderr, code := runLifecycleHarness(t, bin, "send-offer-cancel", env)
				marker := "OK_OFFER_CANCEL: mode=" + mode + " rounds=32 cancelled=32 original=32 final_refs=0"
				if code != 0 || !strings.Contains(stdout, marker) {
					t.Fatalf("offer cancellation failed (mode=%s workers=%s code=%d)\nstdout:\n%s\nstderr:\n%s", mode, workers, code, stdout, stderr)
				}
			})
		}
	}
}

const lifecycleHarnessSendOfferCancelModes = `
#include "rt_channel_lane.h"

#define POLL_SEND_OFFER_CANCEL 4093

typedef struct offer_cancel_state {
    void* channel;
    struct offer_cancel_state* original;
    _Atomic unsigned refs;
    _Atomic unsigned polls;
    _Atomic unsigned moves;
    _Atomic unsigned drops;
    _Atomic unsigned invalid;
    _Atomic unsigned original_observed;
    _Atomic unsigned late_cancels;
    unsigned late_cancel;
    rt_park_token issued;
} offer_cancel_state;

static void offer_cancel_move(void* dst, void* src) {
    offer_cancel_state* state = *(offer_cancel_state**)src;
    *(offer_cancel_state**)dst = state;
    *(offer_cancel_state**)src = NULL;
    if (state != NULL) {
        unsigned prior_moves = atomic_fetch_add_explicit(&state->moves, 1, memory_order_relaxed);
        if (prior_moves == 0) {
            atomic_store_explicit(&state->original_observed,
                                  state->original == state &&
                                      atomic_load_explicit(&state->refs, memory_order_acquire) == 2,
                                  memory_order_release);
        }
        if (state->late_cancel && prior_moves == 0) {
            // This is the real detached staging move, before its commit and
            // before the caller can reach rt_async_yield. No scheduler lock
            // may be held across the public cancellation API.
            rt_task* current = rt_current_task();
            if (current == NULL || rt_lane_holds_control() || rt_lane_holds_any_shard()) {
                atomic_store_explicit(&state->invalid, 1, memory_order_release);
                return;
            }
            rt_task_cancel(current);
            atomic_store_explicit(
                &state->late_cancels, task_cancelled_load(current) != 0, memory_order_release);
        }
    }
}

static void offer_cancel_drop(void* value) {
    offer_cancel_state* state = *(offer_cancel_state**)value;
    *(offer_cancel_state**)value = NULL;
    if (state != NULL) {
        if (atomic_fetch_sub_explicit(&state->refs, 1, memory_order_acq_rel) == 0) {
            atomic_store_explicit(&state->invalid, 1, memory_order_release);
        }
        atomic_fetch_add_explicit(&state->drops, 1, memory_order_relaxed);
    }
}

static rt_carrier_status
offer_cancel_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops offer_cancel_ops = {
    .layout = {.size = sizeof(void*),
               .align = _Alignof(void*),
               .stride = sizeof(void*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = offer_cancel_move,
    .drop_in_place = offer_cancel_drop,
    .plan_cross = offer_cancel_plan_cross,
};

static void poll_send_offer_cancel(void) {
    offer_cancel_state* state = __task_state();
    if (state == NULL || state->original != state) {
        rt_async_return(NULL, &(uint64_t){99});
        return;
    }
    offer_cancel_state* disposable = state->original;
    atomic_fetch_add_explicit(&state->refs, 1, memory_order_relaxed);
    atomic_fetch_add_explicit(&state->polls, 1, memory_order_relaxed);
    int ready = rt_channel_send_offer(state->channel, &disposable);
    if (ready || disposable != NULL || state->original != state) {
        atomic_store_explicit(&state->invalid, 1, memory_order_release);
        rt_async_return(NULL, &(uint64_t){98});
        return;
    }
    // Preserve the capability independently of the task handle consumed by
    // await. The first poll owns the issued token; a retry must not overwrite
    // it with an empty token after a corrected cancellation has reclaimed it.
    if (state->issued.owner == NULL) {
        rt_channel* channel = state->channel;
        rt_shard* owner = channel_owner_shard(ensure_exec(), channel);
        rt_shard_lock(owner);
        state->issued = rt_current_task()->resume_slot;
        rt_shard_unlock(owner);
    }
    // State is driver-owned (type id 0). Cancellation must drain the offered
    // channel reference while the independent original remains readable.
    rt_async_yield(state, 0);
}

static int offer_cancel_wait(rt_executor* ex, rt_task* task, uint8_t status) {
    for (unsigned i = 0; i < 4000; i++) {
        if (task_status_load(task) == status) {
            return 1;
        }
        // The one-worker configuration intentionally has no background worker:
        // this is the runtime's ordinary single-thread scheduler, not a fake
        // TASK_WAITING publication by the stand.
        if (rt_worker_count() == 1) {
            (void)run_ready_one(ex);
        } else {
            sleep_us(1000);
        }
    }
    return task_status_load(task) == status;
}

typedef struct {
    uint64_t live;
    size_t waiters;
    int token_live;
    int reserved;
    int header_state;
    int resume_kind;
    int same_token;
    int park_kind;
    unsigned refs;
    unsigned drops;
} offer_cancel_census;

static offer_cancel_census offer_cancel_snapshot(rt_executor* ex,
                                                 offer_cancel_state* state,
                                                 const rt_task* task,
                                                 uint64_t task_id) {
    offer_cancel_census out = {
        .reserved = -1, .header_state = -1, .resume_kind = -1, .same_token = -1, .park_kind = -1};
    rt_channel* channel = state->channel;
    rt_shard* owner = channel_owner_shard(ex, channel);
    rt_shard_lock(owner);
    out.live = rt_park_pool_live(&channel->parks);
    out.token_live = rt_park_pool_token_is_live(&channel->parks, &state->issued);
    if (out.token_live) {
        out.reserved = channel->parks.slots[state->issued.index].reserved;
        out.header_state = channel->parks.headers[state->issued.index].state;
    }
    // The stand pins both task and channel to shard 0. Before await its live
    // task handle protects these reads; after await task is NULL, never read.
    if (task != NULL) {
        out.resume_kind = task->resume_kind;
        out.same_token = task->resume_slot.owner == state->issued.owner &&
                         task->resume_slot.index == state->issued.index &&
                         task->resume_slot.generation == state->issued.generation;
        out.park_kind = task->park_key.kind;
    }
    uint64_t channel_id = channel_send_key(channel).id;
    for (size_t i = 0; i < owner->waiter_store.len; i++) {
        const waiter* entry = &owner->waiter_store.entries[i];
        if (entry->task_id == task_id && waker_is_channel(entry->key) &&
            entry->key.id == channel_id) {
            out.waiters++;
        }
    }
    rt_shard_unlock(owner);
    out.refs = atomic_load_explicit(&state->refs, memory_order_acquire);
    out.drops = atomic_load_explicit(&state->drops, memory_order_acquire);
    return out;
}

static int mode_send_offer_cancel(rt_executor* ex) {
    const char* late_env = getenv("SURGE_OFFER_CANCEL_LATE");
    int late = late_env != NULL && strcmp(late_env, "1") == 0;
    const char* mode = late ? "late" : "parked";
    unsigned failed_rounds = 0;
    for (unsigned round = 0; round < 32; round++) {
        offer_cancel_state* state = calloc(1, sizeof(*state));
        if (state == NULL) {
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: state allocation failed");
        }
        state->original = state;
        atomic_init(&state->refs, 1);
        atomic_init(&state->polls, 0);
        atomic_init(&state->moves, 0);
        atomic_init(&state->drops, 0);
        atomic_init(&state->invalid, 0);
        atomic_init(&state->original_observed, 0);
        atomic_init(&state->late_cancels, 0);
        state->late_cancel = (unsigned)late;
        state->channel = rt_channel_new(0, &offer_cancel_ops, 0);
        if (state->channel == NULL) {
            free(state);
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: channel allocation failed");
        }
        // Driver publication uses the existing inject queue path and works
        // even while every background worker is asleep.
        rt_task* task = spawn_pinned_with_state(ex, POLL_SEND_OFFER_CANCEL, 0, state);
        if (task == NULL) {
            rt_channel_handle_drop(state->channel);
            free(state);
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: task allocation failed");
        }
        uint64_t task_id = task->id;
        int parked = 0;
        if (!late) {
            parked = offer_cancel_wait(ex, task, TASK_WAITING);
            rt_channel* channel = state->channel;
            rt_shard* owner = channel_owner_shard(ex, channel);
            rt_shard_lock(owner);
            parked = parked && task_status_load(task) == TASK_WAITING &&
                     rt_park_pool_token_is_live(&channel->parks, &state->issued) &&
                     task->park_key.kind == WAKER_CHAN_SEND;
            rt_shard_unlock(owner);
            rt_task_cancel(task);
        }
        if (!offer_cancel_wait(ex, task, TASK_DONE)) {
            // Keep heap state and handles alive on this failure until process
            // exit; a worker that failed to drain must never see freed state.
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: cancelled sender did not drain");
        }
        int original_live =
            state->original == state &&
            atomic_load_explicit(&state->original_observed, memory_order_acquire) == 1 &&
            atomic_load_explicit(&state->moves, memory_order_acquire) == 1;
        unsigned late_cancels = atomic_load_explicit(&state->late_cancels, memory_order_acquire);
        offer_cancel_census done = offer_cancel_snapshot(ex, state, task, task_id);
        uint8_t kind = 0;
        uint64_t bits = 0;
        rt_task_await(task, &kind, &bits); // Consumes the task handle.
        offer_cancel_census awaited = offer_cancel_snapshot(ex, state, NULL, task_id);
        offer_cancel_state* received = NULL;
        int received_value = rt_channel_try_recv(state->channel, &received);
        int received_nonnull = received != NULL;
        if (received_value) {
            offer_cancel_drop(&received);
        }
        unsigned polls = atomic_load_explicit(&state->polls, memory_order_acquire);
        unsigned drops = atomic_load_explicit(&state->drops, memory_order_acquire);
        unsigned refs_before_teardown = atomic_load_explicit(&state->refs, memory_order_acquire);
        rt_channel_handle_drop(state->channel);
        unsigned refs = atomic_load_explicit(&state->refs, memory_order_acquire);
        unsigned drops_after_teardown = atomic_load_explicit(&state->drops, memory_order_acquire);
        // Teardown is cleanup, never the acceptance boundary. An unexpected
        // owner may still hold this backing, so failure must not free it.
        if (refs == 1) {
            offer_cancel_drop(&state->original);
        }
        unsigned final_refs = atomic_load_explicit(&state->refs, memory_order_acquire);
        unsigned invalid = atomic_load_explicit(&state->invalid, memory_order_acquire);
        int safe_to_free = refs == 1 && final_refs == 0;
        if (safe_to_free) {
            free(state);
        }
        fprintf(stderr,
                "OFFER_CANCEL_CENSUS mode=%s round=%u parked=%d late_cancels=%u "
                "original=%d kind=%u polls=%u done_live=%llu done_token=%d "
                "done_reserved=%d done_header=%d done_waiters=%zu done_resume=%d "
                "done_same_token=%d done_park=%d done_refs=%u done_drops=%u "
                "await_live=%llu await_token=%d await_reserved=%d await_header=%d "
                "await_waiters=%zu await_refs=%u await_drops=%u received=%d "
                "received_nonnull=%d refs_before_teardown=%u drops_before_teardown=%u "
                "refs_after_teardown=%u drops_after_teardown=%u invalid=%u final=%u\n",
                mode,
                round,
                parked,
                late_cancels,
                original_live,
                (unsigned)kind,
                polls,
                (unsigned long long)done.live,
                done.token_live,
                done.reserved,
                done.header_state,
                done.waiters,
                done.resume_kind,
                done.same_token,
                done.park_kind,
                done.refs,
                done.drops,
                (unsigned long long)awaited.live,
                awaited.token_live,
                awaited.reserved,
                awaited.header_state,
                awaited.waiters,
                awaited.refs,
                awaited.drops,
                received_value,
                received_nonnull,
                refs_before_teardown,
                drops,
                refs,
                drops_after_teardown,
                invalid,
                final_refs);
        if (!safe_to_free) {
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: unsafe residual ownership retained until exit");
        }
        if ((!late && !parked) || late_cancels != (unsigned)late || !original_live || kind != 2 ||
            done.live != 0 || done.token_live || done.waiters != 0 || done.refs != 1 ||
            done.drops != polls || awaited.live != 0 || awaited.token_live ||
            awaited.waiters != 0 || awaited.refs != 1 || awaited.drops != polls || received_value ||
            received_nonnull || refs_before_teardown != 1 || drops != polls || refs != 1 ||
            drops_after_teardown != polls || invalid != 0 || final_refs != 0) {
            failed_rounds++;
        }
    }
    (void)rt_executor_request_shutdown(ex);
    if (failed_rounds != 0) {
        fprintf(stderr, "OFFER_CANCEL_FAILED mode=%s rounds=32 failed=%u\n", mode, failed_rounds);
        return fail("offer cancel: ownership census failed before channel teardown");
    }
    printf("OK_OFFER_CANCEL: mode=%s rounds=32 cancelled=32 original=32 final_refs=0\n", mode);
    return 0;
}
`
