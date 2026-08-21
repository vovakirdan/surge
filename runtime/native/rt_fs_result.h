#ifndef SURGE_RT_FS_RESULT_H
#define SURGE_RT_FS_RESULT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// The shapes an FsResult<T> can carry, and the constructors that tag them.
//
// A bare member of a union is still a member: fs_make_error writes its case's
// discriminant and puts the error's bytes at that case's payload offset, the
// same as every other arm.

enum {
    FS_ERR_NOT_FOUND = 1,
    FS_ERR_PERMISSION_DENIED = 2,
    FS_ERR_ALREADY_EXISTS = 3,
    FS_ERR_INVALID_PATH = 4,
    FS_ERR_NOT_DIR = 5,
    FS_ERR_NOT_FILE = 6,
    FS_ERR_IS_DIR = 7,
    FS_ERR_INVALID_DATA = 8,
    FS_ERR_IO = 9,
    FS_ERR_UNSUPPORTED = 10,
};

typedef struct FsError {
    void* message;
    void* code;
} FsError;

typedef struct Metadata {
    void* size;
    uint8_t file_type;
    bool readonly;
} Metadata;

typedef struct DirEntry {
    void* name;
    void* path;
    uint8_t file_type;
} DirEntry;

const char* fs_error_message(uint64_t code);
uint64_t fs_error_code_from_errno(int err);
void fs_release_dir_entries(DirEntry* entries, size_t count);

void* fs_make_error(uint64_t code);
void* fs_make_success_ptr(void* payload);
void* fs_make_success_meta(const Metadata* meta);
void* fs_make_success_nothing(void);
void* fs_make_success_u8(uint8_t value);

#endif // SURGE_RT_FS_RESULT_H
