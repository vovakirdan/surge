#include "rt_fs_result.h"

#include "rt.h"

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

// How an FsResult<T> is BUILT, kept apart from what the filesystem calls do.
//
// The net lane already files its result constructors this way (rt_net_result.c)
// and the reason is the same here: these functions are about a union's shape --
// which case, which payload offset -- while the rest of rt_fs.c is about
// syscalls. Mixing them buries the one place a discriminant is written among
// several hundred lines that never touch one.

const char* fs_error_message(uint64_t code) {
    switch (code) {
        case FS_ERR_NOT_FOUND:
            return "NotFound";
        case FS_ERR_PERMISSION_DENIED:
            return "PermissionDenied";
        case FS_ERR_ALREADY_EXISTS:
            return "AlreadyExists";
        case FS_ERR_INVALID_PATH:
            return "InvalidPath";
        case FS_ERR_NOT_DIR:
            return "NotDir";
        case FS_ERR_NOT_FILE:
            return "NotFile";
        case FS_ERR_IS_DIR:
            return "IsDir";
        case FS_ERR_INVALID_DATA:
            return "InvalidData";
        case FS_ERR_UNSUPPORTED:
            return "Unsupported";
        default:
            return "Io";
    }
}

uint64_t fs_error_code_from_errno(int err) {
    switch (err) {
        case ENOENT:
            return FS_ERR_NOT_FOUND;
        case EACCES:
        case EPERM:
            return FS_ERR_PERMISSION_DENIED;
        case EEXIST:
            return FS_ERR_ALREADY_EXISTS;
        case ENOTDIR:
            return FS_ERR_NOT_DIR;
        case EISDIR:
            return FS_ERR_IS_DIR;
        case EINVAL:
        case ENAMETOOLONG:
        case ELOOP:
            return FS_ERR_INVALID_PATH;
        case ENOSYS:
        case EOPNOTSUPP:
            return FS_ERR_UNSUPPORTED;
        default:
            return FS_ERR_IO;
    }
}

// Releases what a run of entries holds, leaving the run itself to its owner.
// A partially built directory listing is abandoned on exactly the paths that
// never reach the caller, so its strings have nobody else to release them.
void fs_release_dir_entries(DirEntry* entries, size_t count) {
    if (entries == NULL) {
        return;
    }
    for (size_t i = 0; i < count; i++) {
        rt_string_free(entries[i].name);
        rt_string_free(entries[i].path);
    }
}

// The error arm of FsResult<T> is the union's bare member, so it carries a
// discriminant like every other case and its bytes sit at that case's payload
// offset. Returning an untagged FsError* left the union with no discriminant
// at all -- see the note on net_make_error for what the caller then read.
#define FS_RESULT_ERROR_CASE 1u

// Every FsResult block is handed to generated code, which stores it into the
// result slot and reads the discriminant through it untested (RV2-DEBT-309):
// a refused block ends the process here instead of answering NULL.
static const uint8_t fs_result_oom[] = "fs result allocation failed";
#define FS_RESULT_ALLOC(tag, align, size)                                                          \
    ((uint8_t*)rt_tag_alloc_or_report(                                                             \
        (tag), (align), (size), fs_result_oom, sizeof(fs_result_oom) - 1))

void* fs_make_error(uint64_t code) {
    size_t payload_align = alignof(FsError);
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = FS_RESULT_ALLOC(FS_RESULT_ERROR_CASE, payload_align, sizeof(FsError));
    FsError err;
    const char* msg = fs_error_message(code);
    err.message = rt_string_from_bytes((const uint8_t*)msg, (uint64_t)strlen(msg));
    err.code = rt_biguint_from_u64(code);
    memcpy(mem + payload_offset, &err, sizeof(err));
    return (void*)mem;
}

void* fs_make_success_ptr(void* payload) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = FS_RESULT_ALLOC(0, payload_align, payload_size);
    memcpy(mem + payload_offset, (const void*)&payload, sizeof(payload));
    return mem;
}

// Metadata is a value composite, so the caller copies the WHOLE struct out of
// the payload region rather than following a pointer stored there. Handing back
// a pointer left the first field — `size`, itself a bignum handle — reading as
// the address of the block, which printed as an enormous number instead of a
// file size.
void* fs_make_success_meta(const Metadata* meta) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = FS_RESULT_ALLOC(0, payload_align, payload_size);
    memcpy(mem + payload_offset, (const void*)meta, sizeof(Metadata));
    return mem;
}

void* fs_make_success_nothing(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = FS_RESULT_ALLOC(0, payload_align, payload_size);
    mem[payload_offset] = 0;
    return mem;
}

void* fs_make_success_u8(uint8_t value) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = FS_RESULT_ALLOC(0, payload_align, payload_size);
    mem[payload_offset] = value;
    return mem;
}
