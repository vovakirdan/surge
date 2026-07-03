#include "rt_async_internal.h"

void rt_trace_collect_waiter_counts(const rt_executor* ex, rt_waiter_trace_counts* out) {
    if (out == NULL) {
        return;
    }
    memset(out, 0, sizeof(*out));
    const rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t shard_index = 0; shard_index < shard_count; shard_index++) {
        const rt_waiter_store* store = rt_executor_waiter_store_const_for_shard(ex, shard_index);
        for (size_t i = 0; store != NULL && i < store->len; i++) {
            waker_kind kind = (waker_kind)store->entries[i].key.kind;
            out->total++;
            if (kind == WAKER_JOIN || kind == WAKER_SCOPE || kind == WAKER_BLOCKING) {
                out->join++;
            } else if (kind == WAKER_TIMER) {
                out->timer++;
            } else if (kind == WAKER_CHAN_SEND) {
                out->chan_send++;
            } else if (kind == WAKER_CHAN_RECV) {
                out->chan_recv++;
            } else if (kind == WAKER_NET_ACCEPT || kind == WAKER_NET_READ ||
                       kind == WAKER_NET_WRITE) {
                out->net++;
            } else {
                out->other++;
            }
        }
    }
}
