#include "channel_claim_retry_stand.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// A descriptor with an independent reference census. Leaving moved-from bits
// intact catches a spurious drop after take; clearing them catches callers
// which pass their original binding as the supposedly disposable offer.
static retry_fixture* offer_fixture;
static uint64_t* offer_address;
static unsigned offer_moves;
static unsigned offer_drops;
static unsigned offer_refs;
static int offer_clear;
static rt_task* offer_receiver_to_kill;

static void offer_move(void* dst, void* src) {
    *(uint64_t*)dst = *(uint64_t*)src;
    if (src == offer_address) {
        offer_moves++;
        if (offer_receiver_to_kill != NULL) {
            task_status_store(offer_receiver_to_kill, TASK_DONE);
            // Opening this rival push before the send selected its receiver
            // would prevent rendezvous altogether: in-flight pushes are FIFO
            // predecessors too. Open it inside the detached stage move.
            hold_ring_push(offer_fixture);
        }
    }
    if (offer_clear) {
        *(uint64_t*)src = 0;
    }
}

static void offer_drop(void* value) {
    if (value == offer_address) {
        offer_drops++;
        if (rt_lane_holds_control() || rt_lane_holds_any_shard() || rt_lane_holds_token_lock()) {
            stand_fail("offer drop held an owner lock");
        }
        if (offer_fixture == NULL || retry_pin_count(offer_fixture) == 0) {
            stand_fail("offer drop ran after operation unpin");
        }
    }
    if (*(uint64_t*)value != 0) {
        if (offer_refs == 0) {
            stand_fail("offer reference was released twice");
        }
        offer_refs--;
    }
    if (offer_clear) {
        *(uint64_t*)value = 0;
    }
}

static const rt_value_ops offer_ops = {
    .layout = {.size = sizeof(uint64_t),
               .align = _Alignof(uint64_t),
               .stride = sizeof(uint64_t),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = offer_move,
    .drop_in_place = offer_drop,
};

static int call_offer(retry_fixture* f, int yield, unsigned take, int ready) {
    uint64_t original = 1;
    uint64_t disposable = original;
    offer_refs++;
    offer_address = &disposable;
    offer_fixture = f;
    offer_moves = 0;
    offer_drops = 0;
#ifdef RV2_SEND_OFFER_OLD_API_NEGATIVE_CONTROL
    int done = yield ? rt_channel_send_yield(f->handle, &disposable)
                     : rt_channel_send(f->handle, &disposable);
#else
    int done = yield ? rt_channel_send_yield_offer(f->handle, &disposable)
                     : rt_channel_send_offer(f->handle, &disposable);
#endif
    offer_address = NULL;
    offer_fixture = NULL;
    if (offer_moves != take || offer_drops != 1 - take) {
        fprintf(
            stderr, "offer census: take=%u moves=%u drops=%u\n", take, offer_moves, offer_drops);
        stand_fail("offer must transfer or drop exactly one reference");
    }
    if (done != ready || original != 1 || (offer_clear && disposable != 0)) {
        stand_fail("offer changed readiness or its separate original owner");
    }
    return done;
}

static void seed_offer_ring(retry_fixture* f) {
    release_held_claim(f);
    uint64_t empty = 0;
    if (!rt_channel_try_send(f->handle, &empty)) {
        stand_fail("offer ring seed was refused");
    }
}

static rt_task* offer_receiver(retry_fixture* f) {
    rt_task* receiver = make_retry_task(f->ex);
    if (receiver == NULL) {
        stand_fail("offer receiver allocation failed");
    }
    rt_shard_lock(f->owner);
    channel_park_prepare_locked(f->owner, receiver, channel_recv_key(f->channel));
    task_status_store(receiver, TASK_WAITING);
    rt_shard_unlock(f->owner);
    return receiver;
}

static void release_offer_slot(retry_fixture* f, rt_task* task) {
    rt_channel_pin(f->handle);
    rt_shard_lock(f->owner);
    channel_end_park_locked(f->ex, f->owner, f->channel, &task->resume_slot);
    task->resume_slot = (rt_park_token){0};
    rt_shard_unlock(f->owner);
    rt_channel_unpin(f->handle);
}

static void offer_pool_full(retry_fixture* f, int yield) {
    seed_offer_ring(f);
    size_t count = (size_t)f->channel->parks.capacity;
    rt_park_token* tokens = calloc(count, sizeof(*tokens));
    if (tokens == NULL || count == 0) {
        stand_fail("offer park pool setup allocation failed");
    }
    rt_shard_lock(f->owner);
    for (size_t i = 0; i < count; i++) {
        if (rt_park_pool_acquire_locked(&f->channel->parks, &tokens[i]) != RT_SLOT_CONTROL_OK) {
            stand_fail("offer park pool was not completely filled");
        }
    }
    rt_shard_unlock(f->owner);
    (void)call_offer(f, yield, 0, 0);
    if (rt_park_pool_token_is_live(&f->channel->parks, &f->task->resume_slot)) {
        stand_fail("full park pool nevertheless took an offer");
    }
    clear_prepared_waiter(f);
    for (size_t i = 0; i < count; i++) {
        // The stand has no workers; these slots contain no value.
        if (rt_park_pool_release(&f->channel->parks, &tokens[i]) != RT_SLOT_CONTROL_OK) {
            stand_fail("empty offer pool slot could not be released");
        }
    }
    free(tokens);
}

void run_send_offer_mode(const char* mode) {
    // Prefixes are consumed in order so every branch runs against both APIs
    // and against both move/drop behaviors without duplicating its setup.
    int yield = strncmp(mode, "yield-", 6) == 0;
    if (yield) {
        mode += 6;
    }
    offer_clear = strncmp(mode, "clear-", 6) == 0;
    if (offer_clear) {
        mode += 6;
    }
    offer_refs = 1; // The separate original binding's reference.
    retry_fixture f = make_fixture_with_ops(&offer_ops);
    rt_task* receiver = NULL;
    if (strcmp(mode, "ack") == 0) {
        f.task->resume_kind = RESUME_CHAN_SEND_ACK;
        (void)call_offer(&f, yield, 0, 1);
        release_held_claim(&f);
    } else if (strcmp(mode, "cancel-before-take") == 0) {
        (void)task_cancel_gate_request(f.task);
        (void)call_offer(&f, yield, 0, 0);
        release_held_claim(&f);
    } else if (strcmp(mode, "claim-refusal") == 0) {
        for (unsigned i = 0; i < RT_CHANNEL_RETRY_BUDGET; i++) {
            (void)call_offer(&f, yield, 0, 0);
        }
        if (!f.task->park_prepared || f.task->channel_retry.count != RT_CHANNEL_RETRY_BUDGET) {
            stand_fail("offer refusal lost the existing bounded retry protocol");
        }
        clear_prepared_waiter(&f);
        release_held_claim(&f);
    } else if (strcmp(mode, "pool-full") == 0) {
        offer_pool_full(&f, yield);
    } else if (strcmp(mode, "ring") == 0) {
        release_held_claim(&f);
        (void)call_offer(&f, yield, 1, 1);
    } else if (strcmp(mode, "rendezvous") == 0) {
        release_held_claim(&f);
        receiver = offer_receiver(&f);
        (void)call_offer(&f, yield, 1, !yield);
        if (!rt_park_pool_token_is_live(&f.channel->parks, &receiver->resume_slot)) {
            stand_fail("rendezvous did not transfer its staged offer");
        }
        if (yield) {
            (void)call_offer(&f, yield, 0, 1);
        }
    } else if (strcmp(mode, "recovery-continue") == 0) {
        release_held_claim(&f);
        receiver = offer_receiver(&f);
        offer_receiver_to_kill = receiver;
        (void)call_offer(&f, yield, 1, 0);
        offer_receiver_to_kill = NULL;
        if (!rt_park_pool_token_is_live(&f.channel->parks, &f.task->resume_slot)) {
            stand_fail("dead-receiver recovery did not preserve the first take");
        }
        release_held_claim(&f);
        (void)call_offer(&f, yield, 0, 1);
    } else if (strcmp(mode, "staged-repoll") == 0) {
        seed_offer_ring(&f);
        (void)call_offer(&f, yield, 1, 0);
        if (!f.task->park_prepared ||
            !rt_park_pool_token_is_live(&f.channel->parks, &f.task->resume_slot)) {
            stand_fail("first offer did not stage and prepare a park");
        }
        clear_prepared_waiter(&f);
        (void)call_offer(&f, yield, 0, 0);
        clear_prepared_waiter(&f);
    } else {
        stand_fail("unknown offer mode");
    }
    if (f.task->park_prepared) {
        clear_prepared_waiter(&f);
    }
    if (receiver != NULL && rt_park_pool_token_is_live(&f.channel->parks, &receiver->resume_slot)) {
        release_offer_slot(&f, receiver);
    }
    if (rt_park_pool_token_is_live(&f.channel->parks, &f.task->resume_slot)) {
        release_offer_slot(&f, f.task);
    }
    finish_fixture(&f);
    if (offer_refs != 1) {
        stand_fail("offer cleanup did not leave exactly the original reference");
    }
    uint64_t original = 1;
    offer_drop(&original);
    if (offer_refs != 0) {
        stand_fail("offer reference survived all owners");
    }
    printf(
        "OK_OFFER: mode=%s yield=%d clear=%d original=1 final_refs=0\n", mode, yield, offer_clear);
    fflush(stdout);
}
