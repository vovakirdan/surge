#ifndef SURGE_RUNTIME_NATIVE_RT_RUNTIME_CONFIG_H
#define SURGE_RUNTIME_NATIVE_RT_RUNTIME_CONFIG_H

#include <stddef.h>
#include <stdint.h>

// Fixed storage bound for Runtime V2 shards. Runtime configuration selects
// 1..RT_RUNTIME_MAX_SHARDS entries from rt_runtime.shards.
#define RT_RUNTIME_MAX_SHARDS 64U

typedef enum {
    RT_RUNTIME_STATUS_OK = 0,
    RT_RUNTIME_STATUS_INVALID_ARGUMENT = 1,
    RT_RUNTIME_STATUS_ALLOCATION_FAILED = 2,
} rt_runtime_status;

typedef struct {
    size_t shard_count;
    uint32_t legacy_worker_threads;
    uint32_t shard_worker_count;
    uint32_t blocking_threads;
} rt_runtime_start_config;

rt_runtime_status rt_runtime_start_config_from_env(rt_runtime_start_config* out,
                                                   const char** error_msg);
void rt_runtime_config_exit(const char* msg);

#endif
