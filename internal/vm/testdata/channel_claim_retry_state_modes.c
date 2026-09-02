#include "channel_claim_retry_stand.h"

#include <stdatomic.h>
#include <stdio.h>
#include <string.h>

size_t retry_waiter_count(retry_fixture* f, waker_key key) {
    size_t count = 0;
    rt_shard_lock(f->owner);
    const rt_waiter_store* store = &f->owner->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        const waiter* row = &store->entries[i];
        if (row->key.kind == key.kind && row->key.id == key.id) {
            count++;
        }
    }
    rt_shard_unlock(f->owner);
    return count;
}

uint32_t retry_pin_count(const retry_fixture* f) {
    return atomic_load_explicit(&f->channel->pin_state, memory_order_acquire) &
           RT_CHANNEL_PIN_COUNT_MASK;
}

void run_select_identity_mode(void) {
    retry_fixture f = make_fixture();
    uint64_t value = 7277;
    const uint8_t kinds[1] = {SELECT_CHAN_SEND};
    void* handles_a[1] = {f.handle};
    void* handles_b[1] = {f.handle};
    void* values[1] = {&value};
    for (uint8_t attempt = 1; attempt <= 8; attempt++) {
        void** handles = (attempt & 1) != 0 ? handles_a : handles_b;
        pending_key = waker_none();
        if (rt_select_poll(1, kinds, handles, values, NULL, -1) != -1 ||
            f.task->channel_retry.count != attempt) {
            stand_fail("select address rotation reset the retry budget");
        }
        if (attempt < 8 && waker_valid(pending_key)) {
            stand_fail("rotating-address select parked before the eighth refusal");
        }
    }
    if (pending_key.kind != WAKER_CHAN_SEND || !f.task->park_prepared ||
        f.task->wait_keys_len != 2) {
        stand_fail("rotating-address select did not arm both wait keys");
    }
    commit_retry_park(&f);
    release_held_claim(&f);
    require_woken(&f);
    resume_for_poll(&f);
    if (rt_select_poll(1, kinds, handles_b, values, NULL, -1) != 0 || value != 0 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE ||
        f.task->channel_retry.count != 0) {
        stand_fail("rotating-address select did not complete and reset");
    }
    printf("OK_SELECT_IDENTITY: addresses=2 refusals=8 woke=1 completed=1\n");
    fflush(stdout);
    finish_fixture(&f);
}

// Two send arms, both channels' single transfer held by the driver: every
// poll is refused on BOTH arms, so the eighth refusal lands on the second arm
// while the first arm's refusals are only remembered. The release that comes
// is on the FIRST arm -- the select must have subscribed to it too.
void run_select_prefix_mode(void) {
    retry_fixture f = make_fixture();
    void* handle_b = rt_channel_new(1, &retry_word_ops, 0);
    if (handle_b == NULL) {
        stand_fail("could not create the second arm");
    }
    rt_channel* channel_b = (rt_channel*)handle_b;
    rt_shard* owner_b = channel_owner_shard(f.ex, channel_b);
    rt_typed_fifo_ticket held_b;
    rt_shard_lock(owner_b);
    if (rt_typed_fifo_reserve_push_locked(&channel_b->ring, &held_b) != RT_SLOT_CONTROL_OK) {
        rt_shard_unlock(owner_b);
        stand_fail("could not hold the second arm's ring claim");
    }
    rt_shard_unlock(owner_b);

    uint64_t value_a = 7277;
    uint64_t value_b = 8277;
    const uint8_t kinds[2] = {SELECT_CHAN_SEND, SELECT_CHAN_SEND};
    void* handles[2] = {f.handle, handle_b};
    void* values[2] = {&value_a, &value_b};
    for (uint8_t poll = 1; poll <= 4; poll++) {
        pending_key = waker_none();
        if (rt_select_poll(2, kinds, handles, values, NULL, -1) != -1 ||
            f.task->channel_retry.count != (uint8_t)(poll * 2)) {
            stand_fail("two-arm select did not count both refusals of a poll");
        }
        if (poll < 4 && waker_valid(pending_key)) {
            stand_fail("two-arm select parked before the eighth refusal");
        }
    }
    if (pending_key.kind != WAKER_CHAN_SEND || !f.task->park_prepared ||
        f.task->channel_retry.prefix_len != 2) {
        stand_fail("two-arm select did not remember both refusing arms");
    }
    commit_retry_park(&f);
    release_held_claim(&f);
    if (task_status_load(f.task) == TASK_WAITING) {
        stand_fail("release on an earlier refusing arm did not wake the select");
    }
    resume_for_poll(&f);
    if (rt_select_poll(2, kinds, handles, values, NULL, -1) != 0 || value_a != 0 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE) {
        stand_fail("woken two-arm select did not commit on the released arm");
    }
    rt_shard_lock(owner_b);
    (void)rt_typed_fifo_abandon_push_locked(&channel_b->ring, &held_b);
    rt_shard_unlock(owner_b);
    rt_channel_handle_drop(handle_b);
    printf("OK_SELECT_PREFIX: arms=2 refusals=8 remembered=2 released=first woke=1 "
           "completed=0\n");
    fflush(stdout);
    finish_fixture(&f);
}

// A select with a `default` arm executes the default immediately when no arm
// won (LANGUAGE.md): it never crosses a suspension point, so a refused claim
// must not republish it and can never park it. Nine polls against a held
// claim, each answering the default index, none leaving state behind.
void run_select_default_mode(void) {
    retry_fixture f = make_fixture();
    uint64_t value = 7277;
    const uint8_t kinds[2] = {SELECT_CHAN_SEND, SELECT_DEFAULT};
    void* handles[2] = {f.handle, NULL};
    void* values[2] = {&value, NULL};
    for (uint8_t poll = 1; poll <= 9; poll++) {
        pending_key = waker_none();
        int64_t selected = rt_select_poll(2, kinds, handles, values, NULL, 1);
        if (selected == -1) {
            stand_fail("select with a default republished on a refused claim");
        }
        if (selected != 1 || value != 7277 || waker_valid(pending_key) || f.task->park_prepared ||
            f.task->wait_keys_len != 0 || f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE ||
            f.task->channel_retry.count != 0) {
            stand_fail("select with a default did not take the default cleanly");
        }
    }
    release_held_claim(&f);
    printf("OK_SELECT_DEFAULT: polls=9 default_wins=9 parked=0 budget=reset\n");
    fflush(stdout);
    finish_fixture(&f);
}

void run_recovery_reset_mode(int foreign_recovery) {
    retry_fixture f = make_fixture();
    release_held_claim(&f);
    uint64_t seed = 6277;
    if (!rt_channel_try_send(f.handle, &seed) || seed != 0) {
        stand_fail("could not seed the recovery ring");
    }
    uint64_t value = 7277;
    pending_key = waker_none();
    if (rt_channel_send(f.handle, &value) || value != 0 || pending_key.kind != WAKER_CHAN_SEND ||
        !f.task->park_prepared ||
        !rt_park_pool_token_is_live(&f.channel->parks, &f.task->resume_slot)) {
        stand_fail("could not stage the recovery value");
    }
    clear_prepared_waiter(&f);
    uint64_t received = 0;
    if (!rt_channel_try_recv(f.handle, &received) || received != 6277) {
        stand_fail("could not drain the recovery seed");
    }
    hold_ring_push(&f);
    drive_direct_refusals(&f, &value);
    add_dead_receiver(&f, foreign_recovery);
    commit_retry_park(&f);
    release_held_claim(&f);
    require_woken(&f);
    resume_for_poll(&f);
    if (!rt_channel_send(f.handle, &value)) {
        stand_fail("staged recovery did not complete");
    }
    if (!rt_channel_try_recv(f.handle, &received) || received != 7277) {
        stand_fail("staged recovery value was not published");
    }
    hold_ring_push(&f);
    uint64_t next = 8277;
    pending_key = waker_none();
    if (rt_channel_send(f.handle, &next) || next != 8277 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_SEND ||
        f.task->channel_retry.count != 1 || waker_valid(pending_key)) {
        stand_fail("new send inherited completed retry budget");
    }
    release_held_claim(&f);
    printf("OK_RECOVERY_RESET: path=%s recovery=completed next_operation_refusals=1 "
           "parked=0\n",
           foreign_recovery ? "foreign" : "same");
    fflush(stdout);
    finish_fixture(&f);
}

void run_park_finish_release_mode(void) {
    retry_fixture f = make_fixture_with_ops(&retry_word_probe_ops);
    release_held_claim(&f);
    uint64_t seed = 6277;
    if (!rt_channel_try_send(f.handle, &seed) || seed != 0) {
        stand_fail("could not seed the park-release ring");
    }
    uint64_t staged = 7277;
    pending_key = waker_none();
    if (rt_channel_send(f.handle, &staged) || staged != 0 || pending_key.kind != WAKER_CHAN_SEND ||
        !f.task->park_prepared ||
        !rt_park_pool_token_is_live(&f.channel->parks, &f.task->resume_slot)) {
        stand_fail("could not stage the park-release value");
    }
    clear_prepared_waiter(&f);
    uint64_t received = 0;
    if (!rt_channel_try_recv(f.handle, &received) || received != 6277) {
        stand_fail("could not drain the park-release seed");
    }
    arm_park_release_probe(&f, 8277);
    rt_park_token released = f.task->resume_slot;
    rt_shard_lock(f.owner);
    channel_end_park_locked(f.ex, f.owner, f.channel, &released);
    rt_shard_unlock(f.owner);
    if (park_release_probe_is_armed()) {
        stand_fail("value-bearing finish-release did not expose its claim");
    }
    disarm_park_release_probe();
    park_current(f.ex, pending_key);
    if (task_status_load(f.task) == TASK_WAITING) {
        stand_fail("finish-release did not preserve retry wake");
    }
    f.task->resume_slot = (rt_park_token){0};
    printf("OK_PARK_FINISH_RELEASE: park_take_refusals=8 wake_before_park=1\n");
    fflush(stdout);
    finish_fixture(&f);
}

static rt_task* prepare_select_sibling(retry_fixture* f, const char* state) {
    rt_task* sibling = make_retry_task(f->ex);
    if (sibling == NULL) {
        stand_fail("could not create select sibling");
    }
    waker_key retry = channel_send_retry_key(f->channel);
    if (strcmp(state, "ready") == 0) {
        waker_key ordinary = channel_send_key(f->channel);
        add_wait_key(f->ex, sibling, ordinary);
        add_wait_key(f->ex, sibling, retry);
        rt_set_current_task(sibling);
        prepare_park(f->ex, sibling, ordinary, 1);
        park_current(f->ex, ordinary);
        rt_set_current_task(f->task);
        wake_key_all(f->ex, ordinary);
        if (task_status_load(sibling) != TASK_READY || task_enqueued_load(sibling) != 1) {
            stand_fail("select sibling did not become ready");
        }
        return sibling;
    }
    add_wait_key(f->ex, sibling, retry);
    if (strcmp(state, "waiting") == 0) {
        rt_set_current_task(sibling);
        prepare_park(f->ex, sibling, retry, 1);
        park_current(f->ex, retry);
        rt_set_current_task(f->task);
        if (task_status_load(sibling) != TASK_WAITING) {
            stand_fail("select sibling did not park");
        }
    }
    return sibling;
}

void run_handoff_mode(const char* state) {
    retry_fixture f = make_fixture();
    rt_task* sibling = prepare_select_sibling(&f, state);
    uint64_t value = 7277;
    drive_direct_refusals(&f, &value);
    commit_retry_park(&f);
    waker_key retry = channel_send_retry_key(f.channel);
    if (retry_waiter_count(&f, retry) != 2 || retry_pin_count(&f) != 2) {
        stand_fail("handoff setup did not own two registrations");
    }
    rt_scheduler* scheduler = rt_shard_scheduler(f.owner);
    size_t ready_before = scheduler->inject.len;
    release_held_claim(&f);
    if (task_status_load(f.task) == TASK_WAITING) {
        stand_fail("claim release stopped at select sibling");
    }
    if (retry_waiter_count(&f, retry) != 0 || retry_pin_count(&f) != 0) {
        stand_fail("handoff did not retire registrations and pins once");
    }
    size_t expected_added = strcmp(state, "waiting") == 0 ? 2 : 1;
    if (scheduler->inject.len != ready_before + expected_added) {
        stand_fail("handoff enqueued an unexpected number of tasks");
    }
    if (strcmp(state, "running") == 0) {
        if (task_status_load(sibling) != TASK_RUNNING || task_enqueued_load(sibling) != 0 ||
            task_wake_token_exchange(sibling, 0) != 1) {
            stand_fail("pre-park select did not receive exactly a wake token");
        }
    } else if (task_status_load(sibling) != TASK_READY || task_enqueued_load(sibling) != 1) {
        stand_fail("select sibling terminal state changed unexpectedly");
    }
    printf("OK_HANDOFF: select=%s registrations=2->0 pins=2->0 direct_woke=1\n", state);
    fflush(stdout);
    finish_fixture(&f);
}

void run_handoff_direct_first_mode(int second_direct) {
    retry_fixture f = make_fixture();
    uint64_t first_value = 7277;
    drive_direct_refusals(&f, &first_value);
    commit_retry_park(&f);
    rt_task* sibling = prepare_select_sibling(&f, second_direct ? "running" : "ready");

    retry_fixture second = f;
    if (second_direct) {
        second.task = make_retry_task(f.ex);
        if (second.task == NULL) {
            stand_fail("could not create second direct waiter");
        }
        rt_set_current_task(second.task);
        uint64_t second_value = 8277;
        drive_direct_refusals(&second, &second_value);
        commit_retry_park(&second);
        rt_set_current_task(f.task);
    }

    waker_key retry = channel_send_retry_key(f.channel);
    size_t initial = second_direct ? 3 : 2;
    if (retry_waiter_count(&f, retry) != initial || retry_pin_count(&f) != initial) {
        stand_fail("direct-first handoff setup ownership mismatch");
    }
    rt_scheduler* scheduler = rt_shard_scheduler(f.owner);
    size_t ready_before = scheduler->inject.len;
    release_held_claim(&f);
    if (task_status_load(f.task) == TASK_WAITING) {
        stand_fail("oldest direct waiter did not receive handoff");
    }
    size_t remaining = second_direct ? 1 : 0;
    if (retry_waiter_count(&f, retry) != remaining || retry_pin_count(&f) != remaining) {
        stand_fail("claim release left select retry subscription behind");
    }
    if (scheduler->inject.len != ready_before + 1) {
        stand_fail("direct-first handoff duplicated select scheduling");
    }
    if (second_direct) {
        if (task_status_load(second.task) != TASK_WAITING ||
            task_status_load(sibling) != TASK_RUNNING || task_enqueued_load(sibling) != 0 ||
            task_wake_token_exchange(sibling, 0) != 1) {
            stand_fail("direct-first handoff did not preserve D2 and signal select");
        }
        hold_ring_push(&f);
        release_held_claim(&f);
        if (task_status_load(second.task) == TASK_WAITING || retry_waiter_count(&f, retry) != 0 ||
            retry_pin_count(&f) != 0) {
            stand_fail("later release did not wake and retire D2");
        }
    } else if (task_status_load(sibling) != TASK_READY || task_enqueued_load(sibling) != 1) {
        stand_fail("direct-first release changed ready select state");
    }
    printf("OK_HANDOFF_ORDER: rows=%s registrations=%zu->%zu pins=%zu->%zu\n",
           second_direct ? "D1,S,D2" : "D1,S",
           initial,
           remaining,
           initial,
           remaining);
    fflush(stdout);
    finish_fixture(&f);
}
