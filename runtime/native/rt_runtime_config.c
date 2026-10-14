#include "rt_async_internal.h"

#include <errno.h>
#include <stdlib.h>
#include <string.h>

static uint32_t rt_env_blocking_count(void) {
    const char* value = getenv("SURGE_BLOCKING_THREADS");
    if (value == NULL || value[0] == '\0') {
        return 0;
    }
    char* end = NULL;
    long parsed = strtol(value, &end, 10);
    if (end == value || parsed <= 0) {
        return 0;
    }
    if ((unsigned long)parsed > UINT32_MAX) { // NOLINT(runtime/int)
        return UINT32_MAX;
    }
    return (uint32_t)parsed;
}

static rt_runtime_status rt_parse_env_size_strict(const char* value, size_t max, size_t* out) {
    if (value == NULL || value[0] == '\0' || value[0] == '-') {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    errno = 0;
    char* end = NULL;
    unsigned long long parsed = strtoull(value, &end, 10);
    if (end == value || *end != '\0' || errno != 0 || parsed < 1 ||
        parsed > (unsigned long long)max) { // NOLINT(runtime/int)
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    *out = (size_t)parsed;
    return RT_RUNTIME_STATUS_OK;
}

static rt_runtime_status rt_env_shard_count(size_t* out, const char** error_msg) {
    if (out == NULL || error_msg == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    const char* value = getenv("SURGE_SHARDS");
    if (value == NULL || value[0] == '\0') {
        *out = rt_runtime_default_shard_count();
        return RT_RUNTIME_STATUS_OK;
    }
    rt_runtime_status status = rt_parse_env_size_strict(value, RT_RUNTIME_MAX_SHARDS, out);
    if (status != RT_RUNTIME_STATUS_OK) {
        *error_msg = "async: invalid SURGE_SHARDS (expected 1..RT_RUNTIME_MAX_SHARDS)";
    }
    return status;
}

rt_runtime_status rt_runtime_start_config_from_env(rt_runtime_start_config* out,
                                                   const char** error_msg) {
    if (out == NULL || error_msg == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    memset(out, 0, sizeof(*out));
    size_t shard_count = 1;
    rt_runtime_status status = rt_env_shard_count(&shard_count, error_msg);
    if (status != RT_RUNTIME_STATUS_OK) {
        return status;
    }
    out->shard_count = shard_count;
    uint32_t blocking_threads = rt_env_blocking_count();
    if (blocking_threads == 0) {
        blocking_threads = rt_runtime_default_blocking_count((uint32_t)shard_count);
    }
    out->blocking_threads = blocking_threads;
    return RT_RUNTIME_STATUS_OK;
}

void rt_runtime_config_exit(const char* msg) {
    const char* text = msg != NULL ? msg : "async: invalid runtime configuration";
    rt_write_stderr((const uint8_t*)text, (uint64_t)strlen(text));
    rt_write_stderr((const uint8_t*)"\n", 1);
    exit(1);
}
