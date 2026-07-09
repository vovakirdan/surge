#ifndef SURGE_RUNTIME_NATIVE_RT_PLACEMENT_H
#define SURGE_RUNTIME_NATIVE_RT_PLACEMENT_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct rt_runtime rt_runtime;
typedef uint64_t rt_placement;

#define RT_PLACEMENT_KIND_BITS 8u
#define RT_PLACEMENT_KIND_MASK UINT64_C(0xff)

typedef enum {
    RT_PLACEMENT_KIND_POOL = 1,
    RT_PLACEMENT_KIND_DISTRIBUTED = 2,
    RT_PLACEMENT_KIND_SHARD = 3,
} rt_placement_kind;

typedef enum {
    RT_PLACEMENT_STATUS_OK = 0,
    RT_PLACEMENT_STATUS_UNSUPPORTED = 1,
    RT_PLACEMENT_STATUS_INVALID_SHARD = 2,
    RT_PLACEMENT_STATUS_INVALID_ARGUMENT = 3,
} rt_placement_status;

typedef struct {
    rt_placement_status status;
    uint32_t shard_id;
    uint8_t kind;
    uint8_t _pad[3];
    uint64_t payload;
} rt_placement_resolution;

struct rt_placement_debug_snapshot {
    uint64_t resolve_attempts;
    uint64_t exact_shard_resolutions;
    uint64_t distributed_resolutions;
    uint64_t distributed_non_caller_resolutions;
    uint64_t invalid_shard_resolutions;
    uint64_t unsupported_resolutions;
};

rt_placement rt_placement_pool(void);
rt_placement rt_placement_distributed(void);
rt_placement rt_placement_shard(uint32_t shard_id);
uint8_t rt_placement_kind_of(rt_placement placement);
uint64_t rt_placement_payload(rt_placement placement);
rt_placement_resolution
rt_placement_resolve(rt_runtime* runtime, rt_placement placement, uint32_t current_shard_id);
struct rt_placement_debug_snapshot rt_placement_debug_snapshot(const rt_runtime* runtime);
void rt_placement_debug_reset(rt_runtime* runtime);

#ifdef __cplusplus
}
#endif

#endif
