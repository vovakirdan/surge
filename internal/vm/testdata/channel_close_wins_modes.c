// The Close-wins stand (rt_channel_claim.h): the receive claim a rendezvous
// takes when it pops a receiver, and what close, commit and abort do to it
// in each owner-lane order. Built with the claim-retry fixture
// (channel_claim_retry.c, channel_claim_retry_state_modes.c) for the task
// factory and the store census; this file owns its channel and its main.

#include "channel_claim_retry_stand.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static uint64_t close_wins_drops;

static void close_wins_move(void* destination, void* source) {
    *(uint64_t*)destination = *(uint64_t*)source;
    *(uint64_t*)source = 0;
}

static void close_wins_drop(void* value) {
    (void)value;
    close_wins_drops++;
}

static rt_carrier_status
close_wins_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops close_wins_ops = {
    .layout = {.size = sizeof(uint64_t),
               .align = _Alignof(uint64_t),
               .stride = sizeof(uint64_t),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = close_wins_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = close_wins_drop,
    .trace = NULL,
    .plan_cross = close_wins_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

typedef struct {
    rt_executor* ex;
    rt_task* sender;
    void* handle;
    rt_channel* channel;
    rt_shard* owner;
} cw_fixture;

static cw_fixture cw_make(uint64_t capacity) {
    cw_fixture f = {0};
    f.ex = ensure_exec();
    f.sender = f.ex != NULL ? make_retry_task(f.ex) : NULL;
    f.handle = rt_channel_new(capacity, &close_wins_ops, 0);
    if (f.ex == NULL || f.sender == NULL || f.handle == NULL) {
        stand_fail("setup refused");
    }
    f.channel = (rt_channel*)f.handle;
    f.owner = channel_owner_shard(f.ex, f.channel);
    rt_set_current_task(f.sender);
    return f;
}

static retry_fixture cw_as_retry(const cw_fixture* f) {
    retry_fixture r = {0};
    r.ex = f->ex;
    r.task = f->sender;
    r.handle = f->handle;
    r.channel = f->channel;
    r.owner = f->owner;
    return r;
}

// A fresh receiver parked on the recv key with a rendezvous-grade (seq != 0)
// registration: prepared under the owner lane, committed by park_current.
static rt_task* cw_park_receiver(cw_fixture* f) {
    rt_task* receiver = make_retry_task(f->ex);
    if (receiver == NULL) {
        stand_fail("could not create a receiver");
    }
    waker_key key = channel_recv_key(f->channel);
    rt_set_current_task(receiver);
    rt_shard_lock(f->owner);
    channel_park_prepare_locked(f->owner, receiver, key);
    rt_shard_unlock(f->owner);
    park_current(f->ex, key);
    rt_set_current_task(f->sender);
    if (task_status_load(receiver) != TASK_WAITING) {
        stand_fail("receiver did not park");
    }
    return receiver;
}

static size_t cw_wakes(const cw_fixture* f) {
    return rt_shard_scheduler(f->owner)->inject.len;
}

static void cw_claim(cw_fixture* f, rt_channel_put* put) {
    rt_channel_pin(f->handle);
    rt_control_lock(f->ex);
    uint8_t status = rt_channel_claim_send_locked(f->ex, f->handle, put);
    rt_control_unlock(f->ex);
    if (status != 1 || put->kind != RT_CHANNEL_PUT_INTO_PARK || !put->has_candidate ||
        f->channel->recv_claim.active == 0) {
        stand_fail("rendezvous claim was not opened");
    }
}

static void cw_commit(cw_fixture* f, rt_channel_put* put, uint64_t* value) {
    rt_value_move_init_detached(&close_wins_ops, put->address, value);
    rt_control_lock(f->ex);
    rt_channel_finish_send_locked(f->ex, f->handle, put);
    rt_control_unlock(f->ex);
    rt_channel_release_orphan_put(f->ex, f->handle, put);
    rt_channel_unpin(f->handle);
}

static void cw_abort(cw_fixture* f, rt_channel_put* put) {
    rt_control_lock(f->ex);
    rt_channel_abandon_send_locked(f->ex, f->handle, put);
    rt_control_unlock(f->ex);
}

static void cw_finish(cw_fixture* f) {
    rt_exec_trace_dump();
    rt_set_current_task(NULL);
    rt_channel_handle_drop(f->handle);
}

static void run_reserve_close_commit(void) {
    cw_fixture f = cw_make(0);
    retry_fixture rf = cw_as_retry(&f);
    rt_task* receiver = cw_park_receiver(&f);
    rt_channel_put put;
    cw_claim(&f, &put);
    size_t wakes = cw_wakes(&f);
    rt_channel_close(f.handle);
    uint64_t value = 7277;
    cw_commit(&f, &put, &value);
    if (receiver->resume_kind == RESUME_CHAN_RECV_VALUE) {
        stand_fail("a receiver was handed a value on a closed channel");
    }
    if (task_status_load(receiver) == TASK_WAITING ||
        receiver->resume_kind != RESUME_CHAN_RECV_CLOSED) {
        stand_fail("close did not settle the claimed receiver as closed");
    }
    if (close_wins_drops != 1) {
        stand_fail("close-won commit did not destroy the payload exactly once");
    }
    if (cw_wakes(&f) != wakes + 1) {
        stand_fail("the claimed receiver was not woken exactly once");
    }
    if (f.channel->recv_claim.active != 0 || f.channel->recv_claim.close_won != 0 ||
        retry_pin_count(&rf) != 0) {
        stand_fail("close-won claim was not retired");
    }
    printf("OK_CLOSE_COMMIT: order=reserve,close,commit receiver=closed drops=1 wakes=1 "
           "claim=retired\n");
    fflush(stdout);
    cw_finish(&f);
}

static void run_reserve_commit_close(void) {
    cw_fixture f = cw_make(0);
    rt_task* receiver = cw_park_receiver(&f);
    rt_channel_put put;
    cw_claim(&f, &put);
    size_t wakes = cw_wakes(&f);
    uint64_t value = 7277;
    cw_commit(&f, &put, &value);
    if (receiver->resume_kind != RESUME_CHAN_RECV_VALUE || cw_wakes(&f) != wakes + 1) {
        stand_fail("committed rendezvous did not publish to the receiver once");
    }
    rt_channel_close(f.handle);
    if (receiver->resume_kind != RESUME_CHAN_RECV_VALUE || close_wins_drops != 0 ||
        cw_wakes(&f) != wakes + 1 || f.channel->recv_claim.active != 0) {
        stand_fail("close disturbed a value published before it");
    }
    printf("OK_COMMIT_CLOSE: order=reserve,commit,close receiver=value drops=0 wakes=1\n");
    fflush(stdout);
    cw_finish(&f);
}

static void run_reserve_close_abort(void) {
    cw_fixture f = cw_make(0);
    retry_fixture rf = cw_as_retry(&f);
    rt_task* receiver = cw_park_receiver(&f);
    rt_channel_put put;
    cw_claim(&f, &put);
    size_t wakes = cw_wakes(&f);
    rt_channel_close(f.handle);
    cw_abort(&f, &put);
    cw_abort(&f, &put);
    rt_channel_unpin(f.handle);
    if (retry_waiter_count(&rf, channel_recv_key(f.channel)) != 0) {
        stand_fail("close-won abort restored a receive registration");
    }
    if (task_status_load(receiver) == TASK_WAITING ||
        receiver->resume_kind != RESUME_CHAN_RECV_CLOSED || cw_wakes(&f) != wakes + 1) {
        stand_fail("abort changed the close terminal state");
    }
    if (close_wins_drops != 0 || f.channel->recv_claim.active != 0 ||
        f.channel->recv_claim.close_won != 0 || retry_pin_count(&rf) != 0) {
        stand_fail("close-won abort did not retire the claim cleanly");
    }
    printf("OK_CLOSE_ABORT: order=reserve,close,abort,abort receiver=closed requeued=0 drops=0 "
           "wakes=1\n");
    fflush(stdout);
    cw_finish(&f);
}

static void run_claim_not_overtaken(void) {
    cw_fixture f = cw_make(0);
    rt_task* first = cw_park_receiver(&f);
    rt_channel_put put;
    cw_claim(&f, &put);
    rt_task* second = cw_park_receiver(&f);
    uint64_t later = 8277;
    if (rt_channel_try_send(f.handle, &later) || later != 8277) {
        stand_fail("a later send overtook the active rendezvous");
    }
    rt_task* late_sender = make_retry_task(f.ex);
    if (late_sender == NULL) {
        stand_fail("could not create the later sender");
    }
    rt_set_current_task(late_sender);
    pending_key = waker_none();
    uint64_t third = 9277;
    if (rt_channel_send(f.handle, &third) || third != 9277 ||
        late_sender->channel_retry.count != 1 || waker_valid(pending_key)) {
        stand_fail("a refused later send did not republish once");
    }
    rt_set_current_task(f.sender);
    uint64_t value = 7277;
    cw_commit(&f, &put, &value);
    if (first->resume_kind != RESUME_CHAN_RECV_VALUE) {
        stand_fail("the claimed receiver did not get its value");
    }
    rt_set_current_task(late_sender);
    if (!rt_channel_send(f.handle, &third) || third != 0 ||
        second->resume_kind != RESUME_CHAN_RECV_VALUE) {
        stand_fail("the released sender did not meet the next receiver");
    }
    rt_set_current_task(f.sender);
    printf("OK_NOT_OVERTAKEN: try_send=refused task_send=republished commit=first retry=second\n");
    fflush(stdout);
    cw_finish(&f);
}

// RV2-DEBT-276: the popped receiver dies inside the staging window. The
// value stays staged and the sender parks holding its slot; a receiver that
// comes later takes exactly that value.
static void run_dead_receiver(void) {
    cw_fixture f = cw_make(0);
    rt_task* receiver = cw_park_receiver(&f);
    task_status_store(receiver, TASK_DONE);
    uint64_t value = 7277;
    pending_key = waker_none();
    if (rt_channel_send(f.handle, &value)) {
        stand_fail("send completed against a dead receiver");
    }
    if (value != 0 || pending_key.kind != WAKER_CHAN_SEND || !f.sender->park_prepared ||
        !rt_park_pool_token_is_live(&f.channel->parks, &f.sender->resume_slot)) {
        stand_fail("recovery did not park the sender holding its slot");
    }
    park_current(f.ex, pending_key);
    if (task_status_load(f.sender) != TASK_WAITING) {
        stand_fail("sender did not park holding its slot");
    }
    rt_set_current_task(NULL);
    uint64_t got = 0;
    if (!rt_channel_try_recv(f.handle, &got) || got != 7277) {
        stand_fail("recovery destroyed the value");
    }
    if (task_status_load(f.sender) == TASK_WAITING || f.sender->resume_kind != RESUME_CHAN_SEND_ACK) {
        stand_fail("the parked sender was not acked");
    }
    rt_set_current_task(f.sender);
    printf("OK_DEAD_RECEIVER: candidate=dead recovery=kept_slot delivered=7277 destroyed=0\n");
    fflush(stdout);
    cw_finish(&f);
}

// RV2-DEBT-279: a claim bracket entered without the operation pin is refused
// by rt_channel_assert_pinned, loudly, before it can claim anything.
static void run_unpinned_claim(void) {
    // No receiver parked: a registration holds a pin of its own, and the
    // assert answers for the count, not for who took it.
    cw_fixture f = cw_make(0);
    rt_channel_put put;
    rt_control_lock(f.ex);
    (void)rt_channel_claim_send_locked(f.ex, f.handle, &put);
    rt_control_unlock(f.ex);
    printf("FAIL: an unpinned claim was admitted\n");
    fflush(stdout);
    _exit(1);
}

// The send lane's own rendezvous, held with the owner lane RELEASED for the
// move (SP_CHANNEL_RENDEZVOUS_CLAIM_BEFORE_MOVE): the popped receiver must
// already be the claim -- pop and open are one hold -- and a close crossing
// the window settles it. The sender then finds close won: it destroys the
// payload and its next pass answers "send on closed channel", which is the
// panic the row expects the process to die with.
#ifdef RT_TEST_SYNC_POINTS
#include "rt_sync_point.h"

#include <pthread.h>

typedef struct {
    cw_fixture* fixture;
    uint64_t value;
} cw_window_race;

static void* cw_send_in_window(void* raw) {
    cw_window_race* race = (cw_window_race*)raw;
    rt_set_current_task(race->fixture->sender);
    pending_key = waker_none();
    (void)rt_channel_send(race->fixture->handle, &race->value);
    rt_set_current_task(NULL);
    return NULL;
}

static void run_claim_window_close(void) {
    cw_fixture f = cw_make(0);
    rt_task* receiver = cw_park_receiver(&f);
    rt_set_current_task(NULL);
    cw_window_race race = {.fixture = &f, .value = 7277};
    rt_sync_point_id point = RT_SYNC_POINT_SP_CHANNEL_RENDEZVOUS_CLAIM_BEFORE_MOVE;
    unsigned before = rt_sync_point_reached_count(point);
    pthread_t thread;
    if (pthread_create(&thread, NULL, cw_send_in_window, &race) != 0) {
        stand_fail("could not start the sender");
    }
    if (!rt_sync_point_wait_until_after(point, before)) {
        stand_fail("the staging window was not reached");
    }
    rt_shard_lock(f.owner);
    int active = f.channel->recv_claim.active;
    rt_shard_unlock(f.owner);
    if (!active) {
        stand_fail("the claim was not open while the lane was released");
    }
    size_t wakes = cw_wakes(&f);
    rt_channel_close(f.handle);
    if (task_status_load(receiver) == TASK_WAITING ||
        receiver->resume_kind != RESUME_CHAN_RECV_CLOSED || cw_wakes(&f) != wakes + 1) {
        stand_fail("close did not settle the claimed receiver in the window");
    }
    printf("OK_CLAIM_WINDOW: claim=open_at_window close=settled receiver=closed wakes=1\n");
    fflush(stdout);
    rt_sync_point_open();
    (void)pthread_join(thread, NULL);
    // Not reached in the tree: the sender's next pass panics on the closed
    // channel and the process dies with that message.
    printf("FAIL: the sender returned from a rendezvous close had won\n");
    fflush(stdout);
    _exit(1);
}
#else
static void run_claim_window_close(void) {
    stand_fail("claim-window mode requires test sync points");
}
#endif

int main(void) {
    const char* mode = getenv("SURGE_CLOSE_WINS_MODE");
    if (mode != NULL && strcmp(mode, "reserve-commit-close") == 0) {
        run_reserve_commit_close();
    } else if (mode != NULL && strcmp(mode, "reserve-close-abort") == 0) {
        run_reserve_close_abort();
    } else if (mode != NULL && strcmp(mode, "claim-not-overtaken") == 0) {
        run_claim_not_overtaken();
    } else if (mode != NULL && strcmp(mode, "dead-receiver") == 0) {
        run_dead_receiver();
    } else if (mode != NULL && strcmp(mode, "unpinned-claim") == 0) {
        run_unpinned_claim();
    } else if (mode != NULL && strcmp(mode, "claim-window-close") == 0) {
        run_claim_window_close();
    } else {
        run_reserve_close_commit();
    }
    _exit(0);
}
