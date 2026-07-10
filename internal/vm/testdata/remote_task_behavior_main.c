#include "remote_task_behavior.h"

#include <string.h>

int main(int argc, char** argv) {
    if (argc != 2)
        return task9_fail("usage: remote_task_behavior <mode>");
    if (strcmp(argv[1], "already-done") == 0)
        return task9_mode_already_done();
    if (strcmp(argv[1], "stale") == 0)
        return task9_mode_stale();
    if (strcmp(argv[1], "race-before") == 0) {
        return task9_mode_registration_race(RT_SYNC_POINT_SP_REMOTE_TASK_BEFORE_OWNER_REGISTER);
    }
    if (strcmp(argv[1], "race-after") == 0) {
        return task9_mode_registration_race(RT_SYNC_POINT_SP_REMOTE_TASK_AFTER_OWNER_REGISTER);
    }
    if (strcmp(argv[1], "teardown") == 0)
        return task9_mode_teardown();
    if (strcmp(argv[1], "pre-ack-cancel") == 0)
        return task9_mode_pre_ack_cancel();
    if (strcmp(argv[1], "queue-failure") == 0)
        return task9_mode_queue_failure();
    if (strcmp(argv[1], "shutdown-waiters") == 0)
        return task9_mode_shutdown_waiters();
    return task9_fail("unknown mode");
}
