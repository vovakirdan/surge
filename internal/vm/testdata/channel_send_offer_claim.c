#include "rt_channel_lane.h"
#include "rt_task_refs.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// The concatenated lifecycle harness supplies these existing definitions.
// These declarations also keep the fragment's standalone C checks honest.
extern void sleep_us(unsigned long micros);
extern rt_task* spawn_pinned_with_state(rt_executor*, int64_t, uint32_t, void*);

#define POLL_SEND_OFFER_CLAIM 4094
#define POLL_RECV_OFFER_CLAIM 4095
void poll_send_offer_claim(void);
void poll_recv_offer_claim(void);
int mode_send_offer_claim(rt_executor* ex);

typedef struct offer_claim_state offer_claim_state;
typedef struct {
    offer_claim_state* owner;
    _Atomic unsigned refs;
    _Atomic unsigned moves;
    _Atomic unsigned drops;
    unsigned tag;
} offer_claim_value;

struct offer_claim_state {
    rt_executor* ex;
    rt_shard* owner;
    void* channel;
    rt_task* sender_handle;
    uint64_t sender_id;
    rt_park_token issued;
    const char* route;
    unsigned round;
    unsigned refill;
    unsigned async;
    offer_claim_value offered;
    offer_claim_value seed;
    offer_claim_value* original;
    _Atomic unsigned callbacks;
    _Atomic unsigned cancelled;
    _Atomic unsigned receives;
    _Atomic unsigned sender_polls;
};

typedef struct {
    uint32_t running;
    uint32_t refs;
    uint64_t live;
    uint64_t buffered;
    rt_park_token resume;
    unsigned present;
    unsigned status;
    unsigned queued;
    unsigned polling;
    unsigned token_live;
    unsigned reserved;
    unsigned header;
} offer_claim_census;

static _Noreturn void offer_claim_fail(const offer_claim_state* state, const char* reason) {
    fprintf(stderr, "FAIL_OFFER_CLAIM: route=%s workers=%llu round=%u %s\n",
            state->route, (unsigned long long)rt_worker_count(), state->round, reason);
    fflush(stdout);
    fflush(stderr);
    // In particular, an unfixed receiver must never resume with its stale
    // sender pointer. A named observer refusal is the negative witness.
    _Exit(1);
}

// Raw locks deliberately do not run lane_run_deferred at unlock. Observing a
// completion must not itself finish the reclamation whose lifetime we test.
static int offer_claim_snapshot(offer_claim_state* state, offer_claim_census* out) {
    int status = pthread_mutex_trylock(&state->ex->lock);
    if (status == EBUSY) return 0;
    if (status != 0) offer_claim_fail(state, "control snapshot lock failed");
    status = pthread_mutex_trylock(&state->owner->lock);
    if (status != 0) {
        if (pthread_mutex_unlock(&state->ex->lock) != 0)
            offer_claim_fail(state, "control snapshot unlock failed");
        if (status != EBUSY) offer_claim_fail(state, "shard snapshot lock failed");
        return 0;
    }
    *out = (offer_claim_census){0};
    out->running = rt_shard_scheduler(state->owner)->running_count;
    const rt_task* sender = get_task(state->ex, state->sender_id);
    out->present = sender != NULL;
    if (sender != NULL) {
        out->refs = atomic_load_explicit(&sender->handle_refs, memory_order_acquire);
        out->status = task_status_load(sender);
        out->queued = task_enqueued_load(sender);
        out->polling = atomic_load_explicit(&sender->polling, memory_order_acquire);
        if (out->status == TASK_WAITING && out->polling == 0) out->resume = sender->resume_slot;
    }
    const rt_channel* channel = state->channel;
    out->live = rt_park_pool_live(&channel->parks);
    out->buffered = channel_buffered(channel);
    out->token_live = (unsigned)rt_park_pool_token_is_live(&channel->parks, &state->issued);
    if (out->token_live) {
        out->reserved = channel->parks.slots[state->issued.index].reserved;
        out->header = channel->parks.headers[state->issued.index].state;
    }
    if (pthread_mutex_unlock(&state->owner->lock) != 0 ||
        pthread_mutex_unlock(&state->ex->lock) != 0)
        offer_claim_fail(state, "snapshot unlock failed");
    return 1;
}

static offer_claim_census offer_claim_wait_tail(offer_claim_state* state, uint32_t running) {
    offer_claim_census out;
    for (unsigned i = 0; i < 4000; i++) {
        if (offer_claim_snapshot(state, &out) && out.running == running) return out;
        sleep_us(1000);
    }
    offer_claim_fail(state, "completion tail did not retire");
}

static void offer_claim_observe(offer_claim_state* state) {
    if (rt_lane_holds_control() || rt_lane_holds_any_shard())
        offer_claim_fail(state, "descriptor ran under scheduler lock");
    const rt_task* receiver = rt_current_task();
    const rt_worker_ctx* worker = tls_worker_ctx;
    if ((receiver != NULL) != (state->async != 0) ||
        (worker != NULL && (worker->ex != state->ex || worker->shard != state->owner ||
                           !state->async || rt_worker_count() == 1)) ||
        (state->async && rt_worker_count() > 1 && worker == NULL))
        offer_claim_fail(state, "callback did not use the selected real runner");
    // One shard/one worker starts no background thread: run_ready_one does
    // not count its control-runner poll. Only the worker-loop receiver keeps
    // running_count at one across the nested await and completion tail.
    uint32_t expected_running = receiver != NULL && worker != NULL ? 1u : 0u;
    offer_claim_census before = offer_claim_wait_tail(state, expected_running);
    if (!before.present || before.status != TASK_WAITING || !before.token_live ||
        before.live != 1 || before.reserved != 1 || before.header != RT_SLOT_CLAIMED)
        offer_claim_fail(state, "second offered move did not hold the real sender claim");
    rt_task* handle = state->sender_handle;
    state->sender_handle = NULL; // This callback is the only awaiter.
    rt_task_cancel(handle);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(handle, &kind, &bits);
    // Never read handle again. Single-worker await restores the receiver
    // after run_until_done temporarily installed the cancelled sender.
    if (kind != 2 || rt_current_task() != receiver || tls_worker_ctx != worker)
        offer_claim_fail(state, "nested cancellation await lost its receiver context");
    offer_claim_census after = offer_claim_wait_tail(state, expected_running);
    printf("OFFER_CLAIM_CALLBACK: route=%s round=%u worker_turn=%u running=%u expected=%u "
           "sender_live=%u refs=%u completed=%u pool=%llu reserved=%u header=%u\n",
           state->route, state->round, (unsigned)(worker != NULL), after.running, expected_running,
           after.present, after.refs & RT_TASK_REFS_COUNT_MASK,
           (unsigned)((after.refs & RT_TASK_REFS_COMPLETED) != 0), (unsigned long long)after.live,
           after.reserved, after.header);
    if (!after.present) offer_claim_fail(state, "sender-retired-during-callback");
    if (after.status != TASK_DONE || after.refs != (RT_TASK_REFS_COMPLETED | 1u) ||
        after.queued != 0 || after.polling != 0 || after.live != 1 ||
        !after.token_live || after.reserved != 1 || after.header != RT_SLOT_CLAIMED)
        offer_claim_fail(state, "completed sender must have exactly one claim pin");
    // The cancelled repoll retained a fresh disposable, which the no-take
    // offer path dropped. The original and claimed payload still own one each.
    if (atomic_load_explicit(&state->sender_polls, memory_order_acquire) != 2 ||
        atomic_load_explicit(&state->offered.drops, memory_order_acquire) != 1 ||
        atomic_load_explicit(&state->offered.refs, memory_order_acquire) != 2)
        offer_claim_fail(state, "cancelled repoll did not retire its unused offer");
    atomic_fetch_add_explicit(&state->cancelled, 1, memory_order_relaxed);
    atomic_fetch_add_explicit(&state->callbacks, 1, memory_order_release);
}

static void offer_claim_move(void* dst, void* src) {
    offer_claim_value* value = *(offer_claim_value**)src;
    if (value != NULL) {
        unsigned moved = atomic_fetch_add_explicit(&value->moves, 1, memory_order_relaxed);
        if (value->tag == 2 && moved == 1) offer_claim_observe(value->owner);
    }
    *(offer_claim_value**)dst = value;
    *(offer_claim_value**)src = NULL;
}

static void offer_claim_drop(void* slot) {
    offer_claim_value* value = *(offer_claim_value**)slot;
    if (value == NULL) {
        return;
    }
    *(offer_claim_value**)slot = NULL;
    if (atomic_fetch_sub_explicit(&value->refs, 1, memory_order_acq_rel) == 0)
        offer_claim_fail(value->owner, "value reference underflow");
    atomic_fetch_add_explicit(&value->drops, 1, memory_order_relaxed);
}

static rt_carrier_status offer_claim_cross(const void* value, rt_cross_mode mode, rt_cross_plan* plan) {
    (void)value;
    (void)mode;
    (void)plan;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops offer_claim_ops = {
    .layout = {.size = sizeof(void*), .align = _Alignof(void*), .stride = sizeof(void*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = offer_claim_move, .drop_in_place = offer_claim_drop, .plan_cross = offer_claim_cross,
};

void poll_send_offer_claim(void) {
    offer_claim_state* state = __task_state();
    offer_claim_value* disposable = state->original;
    atomic_fetch_add_explicit(&disposable->refs, 1, memory_order_relaxed);
    atomic_fetch_add_explicit(&state->sender_polls, 1, memory_order_relaxed);
    if (rt_channel_send_offer(state->channel, (void*)&disposable) || disposable != NULL)
        offer_claim_fail(state, "sender did not park its disposable offer");
    rt_async_yield(state, 0);
}

static void offer_claim_receive(offer_claim_state* state) {
    for (unsigned i = 0; i <= state->refill; i++) {
        offer_claim_value* value = NULL;
        int ready = state->async ? rt_channel_recv(state->channel, (void*)&value)
                                 : rt_channel_try_recv(state->channel, (void*)&value);
        unsigned want = state->refill && i == 0 ? 1u : 2u;
        if (ready != 1 || value == NULL || value->owner != state || value->tag != want)
            offer_claim_fail(state, "receive did not preserve seed then offered order");
        offer_claim_drop((void*)&value);
        atomic_fetch_add_explicit(&state->receives, 1, memory_order_release);
    }
}

void poll_recv_offer_claim(void) {
    offer_claim_state* state = __task_state();
    offer_claim_receive(state);
    rt_async_return(NULL, &(uint64_t){0});
}

static void offer_claim_init_value(offer_claim_value* value, offer_claim_state* owner, unsigned tag) {
    value->owner = owner;
    value->tag = tag;
    atomic_init(&value->refs, 1);
    atomic_init(&value->moves, 0);
    atomic_init(&value->drops, 0);
}

static void offer_claim_park_sender(offer_claim_state* state) {
    state->sender_handle = spawn_pinned_with_state(state->ex, POLL_SEND_OFFER_CLAIM, 0, state);
    if (state->sender_handle == NULL) offer_claim_fail(state, "sender allocation failed");
    state->sender_id = state->sender_handle->id;
    offer_claim_census parked;
    unsigned i;
    for (i = 0; i < 4000; i++) {
        if (offer_claim_snapshot(state, &parked) && parked.present &&
            parked.status == TASK_WAITING && parked.running == 0 && parked.polling == 0) break;
        if (rt_worker_count() == 1) (void)run_ready_one(state->ex);
        else sleep_us(1000);
    }
    if (i == 4000) offer_claim_fail(state, "real sender did not finish its parking turn");
    state->issued = parked.resume;
    parked = offer_claim_wait_tail(state, 0);
    if (!parked.token_live || parked.live != 1 || parked.reserved != 0 ||
        parked.header != RT_SLOT_INITIALIZED || parked.queued != 0 ||
        atomic_load_explicit(&state->offered.refs, memory_order_acquire) != 2 ||
        atomic_load_explicit(&state->offered.moves, memory_order_acquire) != 1 ||
        atomic_load_explicit(&state->offered.drops, memory_order_acquire) != 0 ||
        atomic_load_explicit(&state->sender_polls, memory_order_acquire) != 1)
        offer_claim_fail(state, "parked offer census was not initialized");
}

int mode_send_offer_claim(rt_executor* ex) {
    const char* route = getenv("SURGE_OFFER_CLAIM_ROUTE");
    if (route == NULL || (strcmp(route, "async-direct") != 0 &&
        strcmp(route, "async-refill") != 0 && strcmp(route, "sync-refill") != 0)) return 1;
    for (unsigned round = 0; round < 32; round++) {
        offer_claim_state* state = calloc(1, sizeof(*state));
        if (state == NULL) return 1;
        state->ex = ex;
        state->owner = rt_runtime_shard0(rt_executor_runtime(ex));
        state->route = route;
        state->round = round;
        state->refill = strcmp(route, "async-direct") != 0;
        state->async = strcmp(route, "sync-refill") != 0;
        offer_claim_init_value(&state->offered, state, 2);
        offer_claim_init_value(&state->seed, state, 1);
        state->original = &state->offered;
        atomic_init(&state->callbacks, 0);
        atomic_init(&state->cancelled, 0);
        atomic_init(&state->receives, 0);
        atomic_init(&state->sender_polls, 0);
        state->channel = rt_channel_new(state->refill, &offer_claim_ops, 0);
        if (state->channel == NULL) offer_claim_fail(state, "channel allocation failed");
        if (state->refill) {
            offer_claim_value* seed = &state->seed;
            atomic_fetch_add_explicit(&seed->refs, 1, memory_order_relaxed);
            if (!rt_channel_try_send(state->channel, (void*)&seed) || seed != NULL)
                offer_claim_fail(state, "seed was not moved into the buffer");
        }
        offer_claim_park_sender(state);
        if (state->async) {
            rt_task* receiver = spawn_pinned_with_state(ex, POLL_RECV_OFFER_CLAIM, 0, state);
            if (receiver == NULL) offer_claim_fail(state, "receiver allocation failed");
            uint8_t kind = 0;
            uint64_t bits = 99;
            rt_task_await(receiver, &kind, &bits);
            if (kind != 1 || bits != 0) offer_claim_fail(state, "receiver did not complete");
        } else offer_claim_receive(state);
        offer_claim_census final = offer_claim_wait_tail(state, 0);
        if (final.present || final.live != 0 || final.buffered != 0 ||
            atomic_load_explicit(&state->callbacks, memory_order_acquire) != 1 ||
            atomic_load_explicit(&state->cancelled, memory_order_acquire) != 1 ||
            atomic_load_explicit(&state->receives, memory_order_acquire) != state->refill + 1 ||
            state->original != &state->offered || state->original->tag != 2 ||
            atomic_load_explicit(&state->offered.refs, memory_order_acquire) != 1 ||
            atomic_load_explicit(&state->offered.drops, memory_order_acquire) != 2 ||
            atomic_load_explicit(&state->sender_polls, memory_order_acquire) != 2 ||
            atomic_load_explicit(&state->offered.moves, memory_order_acquire) != state->refill + 2 ||
            atomic_load_explicit(&state->seed.refs, memory_order_acquire) != 1 ||
            atomic_load_explicit(&state->seed.moves, memory_order_acquire) != state->refill * 2 ||
            atomic_load_explicit(&state->seed.drops, memory_order_acquire) != state->refill)
            offer_claim_fail(state, "post-receive owner and pool census failed");
        rt_channel_handle_drop(state->channel);
        offer_claim_drop((void*)&state->original);
        offer_claim_value* seed_original = &state->seed;
        offer_claim_drop((void*)&seed_original);
        if (atomic_load_explicit(&state->offered.refs, memory_order_acquire) != 0 ||
            atomic_load_explicit(&state->offered.drops, memory_order_acquire) != 3 ||
            atomic_load_explicit(&state->seed.refs, memory_order_acquire) != 0)
            offer_claim_fail(state, "original teardown left a reference");
        printf("OFFER_CLAIM_CENSUS: route=%s round=%u sender_gone=1 pool=0 original=1 "
               "sender_polls=2 post_receive_drops=2 final_drops=3 final_refs=0\n",
               route, round);
        free(state);
    }
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) return 1;
    printf("OK_OFFER_CLAIM: route=%s workers=%llu rounds=32 callbacks=32 cancelled=32 original=32 final_refs=0\n",
           route, (unsigned long long)rt_worker_count());
    return 0;
}
