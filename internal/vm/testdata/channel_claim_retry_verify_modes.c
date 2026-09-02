#include "channel_claim_retry_stand.h"
#include "rt_sync_point.h"

#include <pthread.h>
#include <stdio.h>
#include <string.h>

#ifdef RT_TEST_SYNC_POINTS

typedef struct {
    retry_fixture* fixture;
    uint64_t value;
    int64_t selected;
    uint8_t status_after_park;
} select_verify_race;

static void drive_first_seven_select_refusals(retry_fixture* f, uint64_t* value) {
    const uint8_t kinds[1] = {SELECT_CHAN_SEND};
    void* handles[1] = {f->handle};
    void* values[1] = {value};
    for (uint8_t attempt = 1; attempt < RT_CHANNEL_RETRY_BUDGET; attempt++) {
        pending_key = waker_none();
        if (rt_select_poll(1, kinds, handles, values, NULL, -1) != -1 ||
            f->task->channel_retry.count != attempt || waker_valid(pending_key) ||
            f->task->wait_keys_len != 0) {
            stand_fail("select did not reach the eighth refusal cleanly");
        }
    }
}

static void* finish_eighth_select_refusal(void* raw) {
    select_verify_race* race = (select_verify_race*)raw;
    retry_fixture* f = race->fixture;
    const uint8_t kinds[1] = {SELECT_CHAN_SEND};
    void* handles[1] = {f->handle};
    void* values[1] = {&race->value};
    rt_set_current_task(f->task);
    pending_key = waker_none();
    race->selected = rt_select_poll(1, kinds, handles, values, NULL, -1);
    if (race->selected != -1 || f->task->channel_retry.count != RT_CHANNEL_RETRY_BUDGET ||
        pending_key.kind != WAKER_CHAN_SEND || !f->task->park_prepared ||
        f->task->wait_keys_len != 2) {
        stand_fail("eighth refused select did not arm both wait keys");
    }
    park_current(f->ex, pending_key);
    race->status_after_park = task_status_load(f->task);
    rt_set_current_task(NULL);
    return NULL;
}

static void require_no_retry_registration(retry_fixture* f) {
    if (retry_waiter_count(f, channel_send_retry_key(f->channel)) != 0) {
        stand_fail("retry subscription appeared before register window opened");
    }
}

static void require_registration_counts(retry_fixture* f, size_t retry, size_t all_pins) {
    if (retry_waiter_count(f, channel_send_retry_key(f->channel)) != retry ||
        retry_pin_count(f) != all_pins) {
        stand_fail("register-verify ownership count mismatch");
    }
}

void run_register_verify_mode(const char* action) {
    retry_fixture f = make_fixture();
    select_verify_race race = {.fixture = &f, .value = 7277, .selected = -2};
    drive_first_seven_select_refusals(&f, &race.value);
    rt_set_current_task(NULL);

    rt_sync_point_id point = RT_SYNC_POINT_SP_CHANNEL_SELECT_REFUSED_BEFORE_RETRY_REGISTER;
    unsigned before = rt_sync_point_reached_count(point);
    pthread_t thread;
    if (pthread_create(&thread, NULL, finish_eighth_select_refusal, &race) != 0) {
        stand_fail("could not start select register-verify race");
    }
    if (!rt_sync_point_wait_until_after(point, before)) {
        stand_fail("select register-verify window was not reached");
    }
    require_no_retry_registration(&f);

    if (strcmp(action, "release") == 0) {
        release_held_claim(&f);
    } else if (strcmp(action, "close") == 0) {
        rt_channel_close(f.handle);
    } else if (strcmp(action, "busy") != 0) {
        stand_fail("unknown register-verify action");
    }

    rt_sync_point_open();
    if (pthread_join(thread, NULL) != 0) {
        stand_fail("could not join select register-verify race");
    }
    if (rt_sync_point_reached_count(point) != before + 1 ||
        f.task->channel_retry.count != RT_CHANNEL_RETRY_BUDGET) {
        stand_fail("register-verify retried or missed its exact window");
    }

    if (strcmp(action, "busy") == 0) {
        if (race.status_after_park != TASK_WAITING) {
            stand_fail("busy exact refusal did not remain genuinely waiting");
        }
        require_registration_counts(&f, 1, 2);
        release_held_claim(&f);
        if (task_status_load(f.task) == TASK_WAITING) {
            stand_fail("later release did not wake verified retry subscription");
        }
        require_registration_counts(&f, 0, 1);
        clear_wait_keys(f.ex, f.task);
        require_registration_counts(&f, 0, 0);
        printf("OK_REGISTER_VERIFY: action=busy status=waiting->ready "
               "registrations=2->1->0 pins=2->1->0\n");
    } else {
        if (race.status_after_park == TASK_WAITING) {
            stand_fail(strcmp(action, "close") == 0
                           ? "select parked after close crossed empty retry key"
                           : "select parked after release crossed empty retry key");
        }
        require_registration_counts(&f, 1, 2);
        clear_wait_keys(f.ex, f.task);
        require_registration_counts(&f, 0, 0);
        if (strcmp(action, "close") == 0) {
            release_held_claim(&f);
        }
        printf("OK_REGISTER_VERIFY: action=%s status=ready registrations=2->0 pins=2->0\n", action);
    }
    fflush(stdout);
    finish_fixture(&f);
}

#else

void run_register_verify_mode(const char* action) {
    (void)action;
    stand_fail("register-verify mode requires test sync points");
}

#endif
