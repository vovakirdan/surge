#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
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

enum {
    FS_O_READ = 1,
    FS_O_WRITE = 2,
    FS_O_CREATE = 4,
    FS_O_TRUNC = 8,
    FS_O_APPEND = 16,
    FS_O_ALL = FS_O_READ | FS_O_WRITE | FS_O_CREATE | FS_O_TRUNC | FS_O_APPEND,
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

typedef struct FsFile {
    int fd;
    char* path;
    bool closed;
} FsFile;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

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
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    memcpy(mem + payload_offset, (const void*)&payload, sizeof(payload));
    return mem;
}

static void* fs_make_success_nothing(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    mem[payload_offset] = 0;
    return mem;
}

static void* fs_make_success_u8(uint8_t value) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(FsError);
    if (payload_size < sizeof(Metadata)) {
        payload_size = sizeof(Metadata);
    }
    if (payload_size < sizeof(void*)) {
        payload_size = sizeof(void*);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(0, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    mem[payload_offset] = value;
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
    bool need_sep = dir_len > 0 && dir[dir_len - 1] != '/';
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

static bool fs_open_flags_mode(uint32_t flags, int* out_mode) {
    if ((flags & (uint32_t)~FS_O_ALL) != 0) {
        return false;
    }
    bool read = (flags & FS_O_READ) != 0;
    bool write = (flags & FS_O_WRITE) != 0;
    if (!read && !write) {
        return false;
    }
    int mode = 0;
    if (read && write) {
        mode = O_RDWR;
    } else if (write) {
        mode = O_WRONLY;
    } else {
        mode = O_RDONLY;
    }
    if ((flags & FS_O_CREATE) != 0) {
        mode |= O_CREAT;
    }
    if ((flags & FS_O_TRUNC) != 0) {
        mode |= O_TRUNC;
    }
    if ((flags & FS_O_APPEND) != 0) {
        mode |= O_APPEND;
    }
    *out_mode = mode;
    return true;
}

static const char* fs_basename(const char* path, size_t* out_len) {
    if (path == NULL) {
        *out_len = 0;
        return "";
    }
    size_t len = strlen(path);
    if (len == 0) {
        *out_len = 0;
        return path;
    }
    size_t end = len;
    while (end > 0 && path[end - 1] == '/') {
        end--;
    }
    if (end == 0) {
        *out_len = 1;
        return "/";
    }
    size_t start = end;
    while (start > 0 && path[start - 1] != '/') {
        start--;
    }
    *out_len = end - start;
    return path + start;
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
    const struct dirent* ent;
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
    long path_max = pathconf(".", _PC_PATH_MAX);
    if (path_max <= 0) {
        path_max = 4096;
    }
    char* cwd_buf = (char*)malloc((size_t)path_max);
    if (cwd_buf == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    char* cwd = getcwd(cwd_buf, (size_t)path_max);
    if (cwd == NULL) {
        free(cwd_buf);
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    void* str = rt_string_from_bytes((const uint8_t*)cwd, (uint64_t)strlen(cwd));
    free(cwd_buf);
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
    const struct dirent* ent;
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
            void** tmp = (void**)realloc((void*)elems, next * sizeof(void*));
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
        free((void*)elems);
        return fs_make_error(code);
    }
    closedir(dir);
    free(buf);

    void* data = NULL;
    if (len > 0) {
        data = rt_alloc((uint64_t)len * (uint64_t)sizeof(void*), (uint64_t)alignof(void*));
        if (data == NULL) {
            free((void*)elems);
            return fs_make_error(FS_ERR_IO);
        }
        memcpy(data, (const void*)elems, len * sizeof(void*));
    }
    free((void*)elems);

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

void* rt_fs_open(void* path, uint32_t flags) {
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, NULL, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    int mode = 0;
    if (!fs_open_flags_mode(flags, &mode)) {
        free(buf);
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    int fd = open(buf, mode, 0666);
    if (fd < 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    FsFile* file = (FsFile*)rt_alloc((uint64_t)sizeof(FsFile), (uint64_t)alignof(FsFile));
    if (file == NULL) {
        close(fd);
        free(buf);
        return fs_make_error(FS_ERR_IO);
    }
    file->fd = fd;
    file->path = buf;
    file->closed = false;
    return fs_make_success_ptr((void*)file);
}

void* rt_fs_close(void* file) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    f->closed = true;
    int fd = f->fd;
    f->fd = -1;
    if (f->path != NULL) {
        free(f->path);
        f->path = NULL;
    }
    if (close(fd) != 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    return fs_make_success_nothing();
}

void* rt_fs_read(void* file, uint8_t* buf, uint64_t cap) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    if (cap == 0) {
        void* count = rt_biguint_from_u64(0);
        return fs_make_success_ptr(count);
    }
    if (buf == NULL || cap > (uint64_t)SSIZE_MAX) {
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    ssize_t n = -1;
    do {
        n = read(f->fd, buf, (size_t)cap);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)n);
    return fs_make_success_ptr(count);
}

void* rt_fs_write(void* file, const uint8_t* buf, uint64_t len) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    if (len == 0) {
        void* count = rt_biguint_from_u64(0);
        return fs_make_success_ptr(count);
    }
    if (buf == NULL || len > (uint64_t)SSIZE_MAX) {
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    ssize_t n = -1;
    do {
        n = write(f->fd, buf, (size_t)len);
    } while (n < 0 && errno == EINTR);
    if (n < 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)n);
    return fs_make_success_ptr(count);
}

void* rt_fs_seek(void* file, int64_t offset, int64_t whence) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    int wh = 0;
    switch (whence) {
        case 0:
            wh = SEEK_SET;
            break;
        case 1:
            wh = SEEK_CUR;
            break;
        case 2:
            wh = SEEK_END;
            break;
        default:
            return fs_make_error(FS_ERR_INVALID_DATA);
    }
    off_t off = (off_t)offset;
    if ((int64_t)off != offset) {
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    off_t pos = -1;
    do {
        pos = lseek(f->fd, off, wh);
    } while (pos < 0 && errno == EINTR);
    if (pos < 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    void* count = rt_biguint_from_u64((uint64_t)pos);
    return fs_make_success_ptr(count);
}

void* rt_fs_flush(void* file) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    if (fsync(f->fd) != 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    return fs_make_success_nothing();
}

void* rt_fs_read_file(void* path) {
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, NULL, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    int fd = open(buf, O_RDONLY, 0666);
    if (fd < 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    struct stat st;
    if (fstat(fd, &st) != 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        close(fd);
        free(buf);
        return fs_make_error(code);
    }
    if (S_ISDIR(st.st_mode)) {
        close(fd);
        free(buf);
        return fs_make_error(FS_ERR_IS_DIR);
    }
    uint8_t* tmp = NULL;
    size_t cap = 0;
    size_t len = 0;
    int err = 0;
    for (;;) {
        if (len == cap) {
            size_t next = cap == 0 ? 4096 : cap * 2;
            if (next < cap) {
                err = ENOMEM;
                break;
            }
            uint8_t* next_buf = (uint8_t*)realloc(tmp, next);
            if (next_buf == NULL) {
                err = ENOMEM;
                break;
            }
            tmp = next_buf;
            cap = next;
        }
        ssize_t n = read(fd, tmp + len, cap - len);
        if (n < 0) {
            if (errno == EINTR) {
                continue;
            }
            err = errno;
            break;
        }
        if (n == 0) {
            break;
        }
        len += (size_t)n;
    }
    close(fd);
    free(buf);
    if (err != 0) {
        free(tmp);
        return fs_make_error(fs_error_code_from_errno(err));
    }

    void* data = NULL;
    if (len > 0) {
        data = rt_alloc((uint64_t)len, (uint64_t)alignof(uint8_t));
        if (data == NULL) {
            free(tmp);
            return fs_make_error(FS_ERR_IO);
        }
        memcpy(data, tmp, len);
    }
    free(tmp);

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

void* rt_fs_write_file(void* path, const uint8_t* data, uint64_t len, uint32_t flags) {
    if (len > 0 && data == NULL) {
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    uint64_t err_code = 0;
    char* buf = fs_copy_path(path, NULL, &err_code);
    if (buf == NULL) {
        return fs_make_error(err_code == 0 ? FS_ERR_INVALID_PATH : err_code);
    }
    int mode = 0;
    if (!fs_open_flags_mode(flags, &mode)) {
        free(buf);
        return fs_make_error(FS_ERR_INVALID_DATA);
    }
    int fd = open(buf, mode, 0666);
    if (fd < 0) {
        uint64_t code = fs_error_code_from_errno(errno);
        free(buf);
        return fs_make_error(code);
    }
    uint64_t written = 0;
    int err = 0;
    while (written < len) {
        size_t chunk = (size_t)(len - written);
        if (chunk > (size_t)SSIZE_MAX) {
            chunk = (size_t)SSIZE_MAX;
        }
        ssize_t n = write(fd, data + written, chunk);
        if (n < 0) {
            if (errno == EINTR) {
                continue;
            }
            err = errno;
            break;
        }
        if (n == 0) {
            err = EIO;
            break;
        }
        written += (uint64_t)n;
    }
    if (close(fd) != 0 && err == 0) {
        err = errno;
    }
    free(buf);
    if (err != 0) {
        return fs_make_error(fs_error_code_from_errno(err));
    }
    return fs_make_success_nothing();
}

void* rt_fs_file_name(const void* file) {
    const FsFile* f = (const FsFile*)file;
    if (f == NULL || f->closed || f->path == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    size_t name_len = 0;
    const char* name = fs_basename(f->path, &name_len);
    void* str = rt_string_from_bytes((const uint8_t*)name, (uint64_t)name_len);
    if (str == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    return fs_make_success_ptr(str);
}

void* rt_fs_file_type(const void* file) {
    const FsFile* f = (const FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    struct stat st;
    if (fstat(f->fd, &st) != 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    uint8_t file_type = fs_file_type_from_mode(st.st_mode);
    void* res = fs_make_success_u8(file_type);
    if (res == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    return res;
}

void* rt_fs_file_metadata(void* file) {
    FsFile* f = (FsFile*)file;
    if (f == NULL || f->closed) {
        return fs_make_error(FS_ERR_IO);
    }
    struct stat st;
    if (fstat(f->fd, &st) != 0) {
        return fs_make_error(fs_error_code_from_errno(errno));
    }
    uint64_t size = 0;
    if (st.st_size > 0) {
        size = (uint64_t)st.st_size;
    }
    Metadata* meta = (Metadata*)rt_alloc((uint64_t)sizeof(Metadata), (uint64_t)alignof(Metadata));
    if (meta == NULL) {
        return fs_make_error(FS_ERR_IO);
    }
    meta->size = rt_biguint_from_u64(size);
    meta->file_type = fs_file_type_from_mode(st.st_mode);
    meta->readonly = (st.st_mode & 0222) == 0;
    return fs_make_success_ptr((void*)meta);
}
