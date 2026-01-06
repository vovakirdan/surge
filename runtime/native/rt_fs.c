#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <dirent.h>
#include <errno.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

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

enum {
    FS_TYPE_FILE = 0,
    FS_TYPE_DIR = 1,
    FS_TYPE_SYMLINK = 2,
    FS_TYPE_OTHER = 3,
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

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

static size_t fs_align_up(size_t n, size_t align) {
    if (align <= 1) {
        return n;
    }
    size_t r = n % align;
    if (r == 0) {
        return n;
    }
    return n + (align - r);
}

static const char* fs_error_message(uint64_t code) {
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

static uint64_t fs_error_code_from_errno(int err) {
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

static void* fs_make_error(uint64_t code) {
    FsError* err = (FsError*)rt_alloc((uint64_t)sizeof(FsError), (uint64_t)alignof(FsError));
    if (err == NULL) {
        return NULL;
    }
    const char* msg = fs_error_message(code);
    err->message = rt_string_from_bytes((const uint8_t*)msg, (uint64_t)strlen(msg));
    err->code = rt_biguint_from_u64(code);
    return (void*)err;
}

static void* fs_make_success_ptr(void* payload) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = fs_align_up(4, payload_align);
    size_t size = fs_align_up(payload_offset + payload_size, payload_align);
    uint8_t* mem = (uint8_t*)rt_alloc((uint64_t)size, (uint64_t)payload_align);
    if (mem == NULL) {
        return NULL;
    }
    *(uint32_t*)mem = 0;
    *(void**)(mem + payload_offset) = payload;
    return mem;
}

static void* fs_make_success_nothing(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    size_t payload_offset = fs_align_up(4, payload_align);
    size_t size = fs_align_up(payload_offset + payload_size, payload_align);
    uint8_t* mem = (uint8_t*)rt_alloc((uint64_t)size, (uint64_t)payload_align);
    if (mem == NULL) {
        return NULL;
    }
    *(uint32_t*)mem = 0;
    mem[payload_offset] = 0;
    return mem;
}

static char* fs_copy_path(void* path, uint64_t* out_len, uint64_t* err_code) {
    if (err_code != NULL) {
        *err_code = 0;
    }
    uint64_t len = rt_string_len_bytes(path);
    if (len == 0) {
        if (err_code != NULL) {
            *err_code = FS_ERR_INVALID_PATH;
        }
        return NULL;
    }
    const uint8_t* bytes = rt_string_ptr(path);
    if (bytes == NULL) {
        if (err_code != NULL) {
            *err_code = FS_ERR_INVALID_PATH;
        }
        return NULL;
    }
    if (memchr(bytes, 0, (size_t)len) != NULL) {
        if (err_code != NULL) {
            *err_code = FS_ERR_INVALID_PATH;
        }
        return NULL;
    }
    char* buf = (char*)malloc((size_t)len + 1);
    if (buf == NULL) {
        if (err_code != NULL) {
            *err_code = FS_ERR_IO;
        }
        return NULL;
    }
    memcpy(buf, bytes, (size_t)len);
    buf[len] = 0;
    if (out_len != NULL) {
        *out_len = len;
    }
    return buf;
}

static uint8_t fs_file_type_from_mode(mode_t mode) {
    if (S_ISLNK(mode)) {
        return FS_TYPE_SYMLINK;
    }
    if (S_ISDIR(mode)) {
        return FS_TYPE_DIR;
    }
    if (S_ISREG(mode)) {
        return FS_TYPE_FILE;
    }
    return FS_TYPE_OTHER;
}

static char* fs_join_path(const char* dir, uint64_t dir_len, const char* name, size_t name_len) {
    bool need_sep = true;
    if (dir_len == 0) {
        need_sep = false;
    } else if (dir[dir_len - 1] == '/') {
        need_sep = false;
    }
    size_t total = (size_t)dir_len + (need_sep ? 1 : 0) + name_len;
    char* buf = (char*)malloc(total + 1);
    if (buf == NULL) {
        return NULL;
    }
    size_t off = 0;
    if (dir_len > 0) {
        memcpy(buf, dir, (size_t)dir_len);
        off += (size_t)dir_len;
    }
    if (need_sep) {
        buf[off++] = '/';
    }
    if (name_len > 0) {
        memcpy(buf + off, name, name_len);
        off += name_len;
    }
    buf[off] = 0;
    return buf;
}

static int fs_mkdir_all(const char* path) {
    size_t len = strlen(path);
    if (len == 0) {
        return EINVAL;
    }
    char* tmp = (char*)malloc(len + 1);
    if (tmp == NULL) {
        return ENOMEM;
    }
    memcpy(tmp, path, len + 1);
    for (char* p = tmp + 1; *p != 0; p++) {
        if (*p == '/') {
            *p = 0;
            if (mkdir(tmp, 0777) != 0) {
                if (errno != EEXIST) {
                    int err = errno;
                    free(tmp);
                    return err;
                }
            }
            *p = '/';
        }
    }
    if (mkdir(tmp, 0777) != 0) {
        if (errno != EEXIST) {
            int err = errno;
            free(tmp);
            return err;
        }
    }
    free(tmp);
    return 0;
}

static int fs_remove_dir_recursive(const char* path) {
    DIR* dir = opendir(path);
    if (dir == NULL) {
        return errno;
    }
    int err = 0;
    struct dirent* ent;
    while ((ent = readdir(dir)) != NULL) {
        if (strcmp(ent->d_name, ".") == 0 || strcmp(ent->d_name, "..") == 0) {
            continue;
        }
        size_t name_len = strlen(ent->d_name);
        char* child = fs_join_path(path, (uint64_t)strlen(path), ent->d_name, name_len);
        if (child == NULL) {
            err = ENOMEM;
            break;
        }
        struct stat st;
        if (lstat(child, &st) != 0) {
            err = errno;
            free(child);
            break;
        }
        if (S_ISDIR(st.st_mode)) {
            err = fs_remove_dir_recursive(child);
        } else {
            if (unlink(child) != 0) {
                err = errno;
            }
        }
        free(child);
        if (err != 0) {
            break;
        }
    }
    closedir(dir);
    if (err == 0) {
        if (rmdir(path) != 0) {
            err = errno;
        }
    }
    return err;
}

void* rt_fs_cwd(void) {
    char* cwd = getcwd(NULL, 0);
    if (cwd == NULL) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    void* str = rt_string_from_bytes((const uint8_t*)cwd, (uint64_t)strlen(cwd));
    free(cwd);
    return fs_make_success_ptr(str);
}

void* rt_fs_metadata(void* path) {
    uint64_t path_len = 0;
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, &path_len, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    struct stat st;
    if (lstat(buf, &st) != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    uint64_t size = 0;
    if (st.st_size > 0) {
        size = (uint64_t)st.st_size;
    }
    Metadata* meta = (Metadata*)rt_alloc((uint64_t)sizeof(Metadata), (uint64_t)alignof(Metadata));
    if (meta == NULL) {
        free(buf);
        return fs_make_error(FS_ERR_IO);
    }
    meta->size = rt_biguint_from_u64(size);
    meta->file_type = fs_file_type_from_mode(st.st_mode);
    meta->readonly = (st.st_mode & 0222) == 0;
    free(buf);
    return fs_make_success_ptr((void*)meta);
}

void* rt_fs_read_dir(void* path) {
    uint64_t path_len = 0;
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, &path_len, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    DIR* dir = opendir(buf);
    if (dir == NULL) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    size_t cap = 0;
    size_t len = 0;
    void** elems = NULL;
    errno = 0;
    struct dirent* ent;
    while ((ent = readdir(dir)) != NULL) {
        if (strcmp(ent->d_name, ".") == 0 || strcmp(ent->d_name, "..") == 0) {
            continue;
        }
        size_t name_len = strlen(ent->d_name);
        void* name_str = rt_string_from_bytes((const uint8_t*)ent->d_name, (uint64_t)name_len);
        if (name_str == NULL) {
            errno = ENOMEM;
            break;
        }
        char* full = fs_join_path(buf, path_len, ent->d_name, name_len);
        if (full == NULL) {
            errno = ENOMEM;
            break;
        }

        uint8_t file_type = FS_TYPE_OTHER;
#if defined(DT_UNKNOWN) && defined(DT_DIR) && defined(DT_REG) && defined(DT_LNK)
        if (ent->d_type != DT_UNKNOWN) {
            switch (ent->d_type) {
                case DT_DIR:
                    file_type = FS_TYPE_DIR;
                    break;
                case DT_REG:
                    file_type = FS_TYPE_FILE;
                    break;
                case DT_LNK:
                    file_type = FS_TYPE_SYMLINK;
                    break;
                default:
                    file_type = FS_TYPE_OTHER;
                    break;
            }
        } else
#endif
        {
            struct stat st;
            if (lstat(full, &st) == 0) {
                file_type = fs_file_type_from_mode(st.st_mode);
            }
        }

        void* path_str = rt_string_from_bytes((const uint8_t*)full, (uint64_t)strlen(full));
        free(full);
        if (path_str == NULL) {
            errno = ENOMEM;
            break;
        }

        DirEntry* entry =
            (DirEntry*)rt_alloc((uint64_t)sizeof(DirEntry), (uint64_t)alignof(DirEntry));
        if (entry == NULL) {
            errno = ENOMEM;
            break;
        }
        entry->name = name_str;
        entry->path = path_str;
        entry->file_type = file_type;

        if (len >= cap) {
            size_t next = cap == 0 ? 8 : cap * 2;
            void** tmp = (void**)realloc(elems, next * sizeof(void*));
            if (tmp == NULL) {
                errno = ENOMEM;
                break;
            }
            elems = tmp;
            cap = next;
        }
        elems[len++] = entry;
    }
    if (errno != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        closedir(dir);
        free(buf);
        free(elems);
        return fs_make_error(code);
    }
    closedir(dir);
    free(buf);

    void* data = NULL;
    if (len > 0) {
        data = rt_alloc((uint64_t)(len * sizeof(void*)), (uint64_t)alignof(void*));
        if (data == NULL) {
            free(elems);
            return fs_make_error(FS_ERR_IO);
        }
        memcpy(data, elems, len * sizeof(void*));
    }
    free(elems);

    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    header->len = (uint64_t)len;
    header->cap = (uint64_t)len;
    header->data = data;
    return fs_make_success_ptr((void*)header);
}

void* rt_fs_mkdir(void* path, bool recursive) {
    uint64_t path_len = 0;
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, &path_len, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    int err = 0;
    if (recursive) {
        err = fs_mkdir_all(buf);
    } else {
        if (mkdir(buf, 0777) != 0) {
            err = errno;
        }
    }
    free(buf);
    if (err != 0) {
        return fs_make_error(fs_error_code_from_errno(err));
    }
    return fs_make_success_nothing();
}

void* rt_fs_remove_file(void* path) {
    uint64_t path_len = 0;
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, &path_len, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    struct stat st;
    if (lstat(buf, &st) != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    if (S_ISDIR(st.st_mode)) {
        free(buf);
        return fs_make_error(FS_ERR_IS_DIR);
    }
    if (unlink(buf) != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    free(buf);
    return fs_make_success_nothing();
}

void* rt_fs_remove_dir(void* path, bool recursive) {
    uint64_t path_len = 0;
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, &path_len, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    struct stat st;
    if (lstat(buf, &st) != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    if (!S_ISDIR(st.st_mode)) {
        free(buf);
        return fs_make_error(FS_ERR_NOT_DIR);
    }
    int err = 0;
    if (recursive) {
        err = fs_remove_dir_recursive(buf);
    } else {
        if (rmdir(buf) != 0) {
            err = errno;
        }
    }
    free(buf);
    if (err != 0) {
        return fs_make_error(fs_error_code_from_errno(err));
    }
    return fs_make_success_nothing();
}
