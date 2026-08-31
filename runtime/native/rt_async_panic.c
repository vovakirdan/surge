#include "rt_async_internal.h"

#include <stdio.h>
#include <string.h>

void panic_msg(const char* msg) {
    if (msg == NULL) {
        return;
    }
    rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
}

// Names both sides of a double-poll collision: the site codes are the
// rt_poll_entry_site values, and the task snapshot (status/enqueued/
// wake_token are racy reads by construction — the task IS being polled
// twice) narrows which enqueue produced the second poller.
void rt_double_poll_panic(const rt_task* task, uint8_t holder_site, uint8_t entrant_site) {
    char buf[192];
    (void)snprintf(buf,
                   sizeof(buf),
                   "async: double poll (task=%llu kind=%u status=%u enqueued=%u wake_token=%u "
                   "holder_site=%u entrant_site=%u worker=%d)",
                   (unsigned long long)task->id,
                   (unsigned)task->kind,
                   (unsigned)task_status_load(task),
                   (unsigned)task_enqueued_load(task),
                   (unsigned)atomic_load_explicit(&((rt_task*)(uintptr_t)task)->wake_token,
                                                  memory_order_relaxed),
                   (unsigned)holder_site,
                   (unsigned)entrant_site,
                   tls_worker_id);
    panic_msg(buf);
}
