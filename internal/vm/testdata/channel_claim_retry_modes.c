#include "channel_claim_retry_stand.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

void run_direct_mode(void) {
    retry_fixture f = make_fixture();
    uint64_t value = 7277;
    drive_direct_refusals(&f, &value);
    commit_retry_park(&f);
    release_held_claim(&f);
    require_woken(&f);
    resume_for_poll(&f);
    if (!rt_channel_send(f.handle, &value) || value != 0 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE ||
        f.task->channel_retry.count != 0) {
        stand_fail("woken operation did not complete and reset");
    }
    printf("OK_DIRECT: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 "
           "completed=1\n");
    fflush(stdout);
    finish_fixture(&f);
}

void run_select_mode(void) {
    retry_fixture f = make_fixture();
    uint64_t value = 7277;
    const uint8_t kinds[1] = {SELECT_CHAN_SEND};
    void* handles[1] = {f.handle};
    void* values[1] = {&value};
    for (uint8_t attempt = 1; attempt <= 8; attempt++) {
        pending_key = waker_none();
        if (rt_select_poll(1, kinds, handles, values, NULL, -1) != -1 ||
            f.task->channel_retry.count != attempt) {
            stand_fail("select did not preserve its logical retry operation");
        }
        if (attempt < 8 && waker_valid(pending_key)) {
            stand_fail("select parked before the eighth refusal");
        }
    }
    if (pending_key.kind != WAKER_CHAN_SEND || !f.task->park_prepared ||
        f.task->wait_keys_len != 2) {
        stand_fail("select did not arm readiness and claim-release keys");
    }
    commit_retry_park(&f);
    release_held_claim(&f);
    require_woken(&f);
    resume_for_poll(&f);
    if (rt_select_poll(1, kinds, handles, values, NULL, -1) != 0 || value != 0 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE) {
        stand_fail("woken select did not commit and reset");
    }
    printf("OK_SELECT: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 "
           "completed=1\n");
    fflush(stdout);
    finish_fixture(&f);
}

void run_recv_mode(void) {
    retry_fixture f = make_fixture();
    turn_held_push_into_pop(&f);
    uint64_t received = 0;
    for (uint8_t attempt = 1; attempt <= 8; attempt++) {
        pending_key = waker_none();
        if (rt_channel_recv(f.handle, &received) != 0 || f.task->channel_retry.count != attempt) {
            stand_fail("recv did not preserve its logical retry operation");
        }
        if (attempt < 8 && waker_valid(pending_key)) {
            stand_fail("recv parked before the eighth refusal");
        }
    }
    if (pending_key.kind != WAKER_CHAN_RECV_RETRY || !f.task->park_prepared) {
        stand_fail("recv did not park on the eighth refusal");
    }
    commit_retry_park(&f);
    release_held_pop(&f);
    require_woken(&f);
    uint64_t replacement = 8277;
    if (!rt_channel_try_send(f.handle, &replacement) || replacement != 0) {
        stand_fail("replacement value could not be published");
    }
    resume_for_poll(&f);
    if (rt_channel_recv(f.handle, &received) != 1 || received != 8277 ||
        f.task->channel_retry.operation != RT_CHANNEL_RETRY_NONE ||
        f.task->channel_retry.count != 0) {
        stand_fail("woken recv did not complete and reset");
    }
    printf("OK_RECV: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 "
           "completed=1\n");
    fflush(stdout);
    finish_fixture(&f);
}

void run_close_mode(void) {
    retry_fixture f = make_fixture();
    uint64_t value = 7277;
    drive_direct_refusals(&f, &value);
    commit_retry_park(&f);
    waker_key retry = channel_send_retry_key(f.channel);
    if (retry_waiter_count(&f, retry) != 1 || retry_pin_count(&f) != 1) {
        stand_fail("close setup did not own one retry registration");
    }
    rt_channel_close(f.handle);
    if (task_status_load(f.task) == TASK_WAITING ||
        f.task->resume_kind != RESUME_CHAN_SEND_CLOSED) {
        stand_fail("close did not terminate the retry park");
    }
    if (retry_waiter_count(&f, retry) != 0 || retry_pin_count(&f) != 0) {
        stand_fail("close did not retire retry registration and pin once");
    }
    release_held_claim(&f);
    printf("OK_CLOSE: retry_park_terminated=1 resume=closed registrations=1->0 pins=1->0\n");
    fflush(stdout);
    finish_fixture(&f);
}

int main(void) {
    const char* mode = getenv("SURGE_CHANNEL_RETRY_MODE");
    if (mode != NULL && strcmp(mode, "select") == 0) {
        run_select_mode();
    } else if (mode != NULL && strcmp(mode, "select-identity") == 0) {
        run_select_identity_mode();
    } else if (mode != NULL && strcmp(mode, "select-prefix") == 0) {
        run_select_prefix_mode();
    } else if (mode != NULL && strcmp(mode, "select-default") == 0) {
        run_select_default_mode();
    } else if (mode != NULL && strcmp(mode, "recovery-reset") == 0) {
        run_recovery_reset_mode(0);
    } else if (mode != NULL && strcmp(mode, "recovery-reset-foreign") == 0) {
        run_recovery_reset_mode(1);
    } else if (mode != NULL && strcmp(mode, "park-finish-release") == 0) {
        run_park_finish_release_mode();
    } else if (mode != NULL && strncmp(mode, "verify-", 7) == 0) {
        run_register_verify_mode(mode + 7);
    } else if (mode != NULL && strncmp(mode, "handoff-", 8) == 0) {
        if (strcmp(mode, "handoff-direct-first") == 0) {
            run_handoff_direct_first_mode(0);
        } else if (strcmp(mode, "handoff-direct-select-direct") == 0) {
            run_handoff_direct_first_mode(1);
        } else {
            run_handoff_mode(mode + 8);
        }
    } else if (mode != NULL && strcmp(mode, "recv") == 0) {
        run_recv_mode();
    } else if (mode != NULL && strcmp(mode, "close") == 0) {
        run_close_mode();
    } else {
        run_direct_mode();
    }
    _exit(0);
}
