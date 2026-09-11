//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

func TestRuntimeV2ChannelSendOfferRepeatedParkedCancellation(t *testing.T) {
	bin := buildRuntimeV2LifecycleHarness(t)
	for _, workers := range []string{"1", "8"} {
		t.Run("workers-"+workers, func(t *testing.T) {
			env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS="+workers, "SURGE_BLOCKING_THREADS=1")
			stdout, stderr, code := runLifecycleHarness(t, bin, "send-offer-cancel", env)
			if code != 0 || !strings.Contains(stdout, "OK_OFFER_CANCEL: rounds=32 parked=32 cancelled=32 original=32 final_refs=0") {
				t.Fatalf("parked offer cancellation failed (workers=%s code=%d)\nstdout:\n%s\nstderr:\n%s", workers, code, stdout, stderr)
			}
		})
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
} offer_cancel_state;

static void offer_cancel_move(void* dst, void* src) {
    offer_cancel_state* state = *(offer_cancel_state**)src;
    *(offer_cancel_state**)dst = state;
    *(offer_cancel_state**)src = NULL;
    if (state != NULL) {
        atomic_fetch_add_explicit(&state->moves, 1, memory_order_relaxed);
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
    .layout = {.size = sizeof(void*), .align = _Alignof(void*),
               .stride = sizeof(void*), .flags = RT_VALUE_FLAG_DROPPABLE},
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

static int mode_send_offer_cancel(rt_executor* ex) {
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
        int parked = offer_cancel_wait(ex, task, TASK_WAITING);
        rt_channel* channel = state->channel;
        rt_shard* owner = channel_owner_shard(ex, channel);
        rt_shard_lock(owner);
        parked = parked && task_status_load(task) == TASK_WAITING &&
                 rt_park_pool_token_is_live(&channel->parks, &task->resume_slot) &&
                 task->park_key.kind == WAKER_CHAN_SEND;
        rt_shard_unlock(owner);
        int original_live = state->original == state &&
            atomic_load_explicit(&state->refs, memory_order_acquire) == 2 &&
            atomic_load_explicit(&state->moves, memory_order_acquire) == 1;
        rt_task_cancel(task);
        if (!offer_cancel_wait(ex, task, TASK_DONE)) {
            // Keep heap state and handles alive on this failure until process
            // exit; a worker that failed to drain must never see freed state.
            (void)rt_executor_request_shutdown(ex);
            return fail("offer cancel: cancelled parked sender did not drain");
        }
        uint8_t kind = 0;
        uint64_t bits = 0;
        rt_task_await(task, &kind, &bits); // Consumes the task handle.
        unsigned polls = atomic_load_explicit(&state->polls, memory_order_acquire);
        unsigned drops = atomic_load_explicit(&state->drops, memory_order_acquire);
        unsigned invalid = atomic_load_explicit(&state->invalid, memory_order_acquire);
        rt_channel_handle_drop(state->channel);
        unsigned refs = atomic_load_explicit(&state->refs, memory_order_acquire);
        offer_cancel_drop(&state->original);
        unsigned final_refs = atomic_load_explicit(&state->refs, memory_order_acquire);
        free(state);
        if (!parked || !original_live || kind != 2 || refs != 1 ||
            drops != polls || invalid != 0 || final_refs != 0) {
            (void)rt_executor_request_shutdown(ex);
            fprintf(stderr, "offer cancel census: round=%u parked=%d original=%d kind=%u "
                            "refs=%u polls=%u drops=%u invalid=%u final=%u\n",
                    round, parked, original_live, (unsigned)kind, refs, polls, drops, invalid, final_refs);
            return fail("offer cancel: parked ownership census failed");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    puts("OK_OFFER_CANCEL: rounds=32 parked=32 cancelled=32 original=32 final_refs=0");
    return 0;
}
`
