#include "rt_async_internal.h"

#include <stdint.h>
#include <string.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

void __surge_poll_call(uint64_t fn_id) {
    (void)fn_id;
}

void __surge_blocking_call(uint64_t fn_id, void* state, void* out_dst) {
    (void)fn_id;
    (void)state;
    (void)out_dst;
}

int main(int argc, char** argv) {
    if (ensure_exec() == NULL) {
        return 90;
    }
    rt_task current;
    rt_task target;
    memset(&current, 0, sizeof(current));
    memset(&target, 0, sizeof(target));
    void* channel = rt_channel_new(1, rt_channel_opaque_word_ops(), 0);
    if (channel == NULL) {
        return 92;
    }
    rt_set_current_task(&current);
    uint64_t value = 7;
    if (argc == 2 && strcmp(argv[1], "send") == 0) {
        rt_channel_send_blocking(channel, &value);
        return 93;
    }
    if (argc == 2 && strcmp(argv[1], "recv") == 0) {
        (void)rt_channel_recv_blocking(channel, &value);
        return 94;
    }
    rt_task_await(&target, NULL, NULL);
    return 91;
}
