#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <ctype.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

typedef enum TermEventKind {
    TERM_EVENT_KIND_KEY = 0,
    TERM_EVENT_KIND_RESIZE = 1,
    TERM_EVENT_KIND_EOF = 2,
} TermEventKind;

typedef enum TermKeyKind {
    TERM_KEY_KIND_CHAR = 0,
    TERM_KEY_KIND_ENTER = 1,
    TERM_KEY_KIND_ESC = 2,
    TERM_KEY_KIND_BACKSPACE = 3,
    TERM_KEY_KIND_TAB = 4,
    TERM_KEY_KIND_UP = 5,
    TERM_KEY_KIND_DOWN = 6,
    TERM_KEY_KIND_LEFT = 7,
    TERM_KEY_KIND_RIGHT = 8,
    TERM_KEY_KIND_HOME = 9,
    TERM_KEY_KIND_END = 10,
    TERM_KEY_KIND_PAGE_UP = 11,
    TERM_KEY_KIND_PAGE_DOWN = 12,
    TERM_KEY_KIND_DELETE = 13,
    TERM_KEY_KIND_F = 14,
} TermKeyKind;

typedef struct TermKeyData {
    TermKeyKind kind;
    uint32_t ch;
    uint8_t f;
} TermKeyData;

typedef struct TermEventSpec {
    TermEventKind kind;
    TermKeyData key;
    uint8_t mods;
    int64_t cols;
    int64_t rows;
} TermEventSpec;

typedef struct TermKeyEventPayload {
    void* key;
    uint8_t mods;
} TermKeyEventPayload;

typedef struct TermResizePayload {
    void* cols;
    void* rows;
} TermResizePayload;

enum {
    TERM_KEY_TAG_CHAR = 0,
    TERM_KEY_TAG_ENTER = 1,
    TERM_KEY_TAG_ESC = 2,
    TERM_KEY_TAG_BACKSPACE = 3,
    TERM_KEY_TAG_TAB = 4,
    TERM_KEY_TAG_UP = 5,
    TERM_KEY_TAG_DOWN = 6,
    TERM_KEY_TAG_LEFT = 7,
    TERM_KEY_TAG_RIGHT = 8,
    TERM_KEY_TAG_HOME = 9,
    TERM_KEY_TAG_END = 10,
    TERM_KEY_TAG_PAGE_UP = 11,
    TERM_KEY_TAG_PAGE_DOWN = 12,
    TERM_KEY_TAG_DELETE = 13,
    TERM_KEY_TAG_F = 14,
};

enum {
    TERM_EVENT_TAG_KEY = 0,
    TERM_EVENT_TAG_RESIZE = 1,
    TERM_EVENT_TAG_EOF = 2,
};

static void* term_make_key(TermKeyData key) {
    size_t payload_align = alignof(uint32_t);
    size_t payload_size = sizeof(uint32_t);
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc((uint32_t)key.kind, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    switch (key.kind) {
        case TERM_KEY_KIND_CHAR:
            *(uint32_t*)(mem + payload_offset) = key.ch;
            break;
        case TERM_KEY_KIND_F:
            *(uint8_t*)(mem + payload_offset) = key.f;
            break;
        default:
            break;
    }
    return mem;
}

static void* term_make_event_key(TermKeyData key, uint8_t mods) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(TermResizePayload);
    if (payload_size < sizeof(TermKeyEventPayload)) {
        payload_size = sizeof(TermKeyEventPayload);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(TERM_EVENT_TAG_KEY, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    TermKeyEventPayload payload = {0};
    payload.key = term_make_key(key);
    payload.mods = mods;
    memcpy(mem + payload_offset, &payload, sizeof(payload));
    return mem;
}

static void* term_make_event_resize(int64_t cols, int64_t rows) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(TermResizePayload);
    if (payload_size < sizeof(TermKeyEventPayload)) {
        payload_size = sizeof(TermKeyEventPayload);
    }
    size_t payload_offset = rt_tag_payload_offset(payload_align);
    uint8_t* mem = (uint8_t*)rt_tag_alloc(TERM_EVENT_TAG_RESIZE, payload_align, payload_size);
    if (mem == NULL) {
        return NULL;
    }
    TermResizePayload payload = {0};
    payload.cols = rt_bigint_from_i64(cols);
    payload.rows = rt_bigint_from_i64(rows);
    memcpy(mem + payload_offset, &payload, sizeof(payload));
    return mem;
}

static void* term_make_event_eof(void) {
    size_t payload_align = alignof(void*);
    size_t payload_size = sizeof(TermResizePayload);
    if (payload_size < sizeof(TermKeyEventPayload)) {
        payload_size = sizeof(TermKeyEventPayload);
    }
    return rt_tag_alloc(TERM_EVENT_TAG_EOF, payload_align, payload_size);
}

static bool term_parse_i64(const char* text, int64_t* out) {
    if (text == NULL || out == NULL) {
        return false;
    }
    char* end = NULL;
    long val = strtol(text, &end, 10);
    if (end == text) {
        return false;
    }
    *out = (int64_t)val;
    return true;
}

static bool term_parse_u8(const char* text, uint8_t* out) {
    if (text == NULL || out == NULL) {
        return false;
    }
    char* end = NULL;
    long val = strtol(text, &end, 10);
    if (end == text || val < 0 || val > 255) {
        return false;
    }
    *out = (uint8_t)val;
    return true;
}

static const char* term_skip_space(const char* p) {
    if (p == NULL) {
        return p;
    }
    while (*p != 0 && isspace((unsigned char)*p)) {
        p++;
    }
    return p;
}

static bool term_parse_resize(const char* token, TermEventSpec* out) {
    const char* p = token + 7;
    p = term_skip_space(p);
    char* end = NULL;
    long cols = strtol(p, &end, 10);
    if (end == p) {
        return false;
    }
    char sep = *end;
    if (sep != 'x' && sep != 'X' && sep != ',') {
        return false;
    }
    p = end + 1;
    p = term_skip_space(p);
    long rows = strtol(p, &end, 10);
    if (end == p) {
        return false;
    }
    out->kind = TERM_EVENT_KIND_RESIZE;
    out->cols = (int64_t)cols;
    out->rows = (int64_t)rows;
    return true;
}

static bool term_key_from_name(const char* name, TermKeyData* out) {
    if (name == NULL || out == NULL) {
        return false;
    }
    if (strcmp(name, "enter") == 0) {
        out->kind = TERM_KEY_KIND_ENTER;
        return true;
    }
    if (strcmp(name, "esc") == 0) {
        out->kind = TERM_KEY_KIND_ESC;
        return true;
    }
    if (strcmp(name, "backspace") == 0) {
        out->kind = TERM_KEY_KIND_BACKSPACE;
        return true;
    }
    if (strcmp(name, "tab") == 0) {
        out->kind = TERM_KEY_KIND_TAB;
        return true;
    }
    if (strcmp(name, "up") == 0) {
        out->kind = TERM_KEY_KIND_UP;
        return true;
    }
    if (strcmp(name, "down") == 0) {
        out->kind = TERM_KEY_KIND_DOWN;
        return true;
    }
    if (strcmp(name, "left") == 0) {
        out->kind = TERM_KEY_KIND_LEFT;
        return true;
    }
    if (strcmp(name, "right") == 0) {
        out->kind = TERM_KEY_KIND_RIGHT;
        return true;
    }
    if (strcmp(name, "home") == 0) {
        out->kind = TERM_KEY_KIND_HOME;
        return true;
    }
    if (strcmp(name, "end") == 0) {
        out->kind = TERM_KEY_KIND_END;
        return true;
    }
    if (strcmp(name, "page_up") == 0) {
        out->kind = TERM_KEY_KIND_PAGE_UP;
        return true;
    }
    if (strcmp(name, "page_down") == 0) {
        out->kind = TERM_KEY_KIND_PAGE_DOWN;
        return true;
    }
    if (strcmp(name, "delete") == 0) {
        out->kind = TERM_KEY_KIND_DELETE;
        return true;
    }
    return false;
}

static bool term_parse_key(const char* token, TermEventSpec* out) {
    const char* p = token + 4;
    p = term_skip_space(p);
    const char* mods_pos = strstr(p, ",mods=");
    uint8_t mods = 0;
    if (mods_pos != NULL) {
        if (!term_parse_u8(mods_pos + 6, &mods)) {
            return false;
        }
    }
    size_t key_len = mods_pos ? (size_t)(mods_pos - p) : strlen(p);
    if (key_len == 0 || key_len >= 64) {
        return false;
    }
    char key_buf[64];
    memcpy(key_buf, p, key_len);
    key_buf[key_len] = 0;

    TermKeyData key = {0};
    if (strncmp(key_buf, "char=", 5) == 0) {
        int64_t value = 0;
        if (!term_parse_i64(key_buf + 5, &value) || value < 0 || value > 0xFFFFFFFFLL) {
            return false;
        }
        key.kind = TERM_KEY_KIND_CHAR;
        key.ch = (uint32_t)value;
    } else if (strncmp(key_buf, "f=", 2) == 0) {
        uint8_t value = 0;
        if (!term_parse_u8(key_buf + 2, &value)) {
            return false;
        }
        key.kind = TERM_KEY_KIND_F;
        key.f = value;
    } else if (!term_key_from_name(key_buf, &key)) {
        return false;
    }

    out->kind = TERM_EVENT_KIND_KEY;
    out->key = key;
    out->mods = mods;
    return true;
}

static bool term_parse_event(const char* token, TermEventSpec* out) {
    if (token == NULL || out == NULL) {
        return false;
    }
    token = term_skip_space(token);
    if (*token == 0) {
        return false;
    }
    if (strcmp(token, "eof") == 0) {
        out->kind = TERM_EVENT_KIND_EOF;
        return true;
    }
    if (strncmp(token, "resize:", 7) == 0) {
        return term_parse_resize(token, out);
    }
    if (strncmp(token, "key:", 4) == 0) {
        return term_parse_key(token, out);
    }
    return false;
}

static char* term_events_buf = NULL;
static char* term_events_next = NULL;
static bool term_events_inited = false;

static void term_events_init(void) {
    if (term_events_inited) {
        return;
    }
    term_events_inited = true;
    const char* env = getenv("SURGE_TERM_EVENTS");
    if (env == NULL || env[0] == 0) {
        return;
    }
    size_t len = strlen(env);
    term_events_buf = (char*)malloc(len + 1);
    if (term_events_buf == NULL) {
        return;
    }
    memcpy(term_events_buf, env, len + 1);
    term_events_next = term_events_buf;
}

static bool term_next_event(TermEventSpec* out) {
    term_events_init();
    if (term_events_next == NULL || out == NULL) {
        return false;
    }
    while (*term_events_next == ';') {
        term_events_next++;
    }
    if (*term_events_next == 0) {
        return false;
    }
    const char* token = term_events_next;
    char* sep = strchr(term_events_next, ';');
    if (sep != NULL) {
        *sep = 0;
        term_events_next = sep + 1;
    } else {
        term_events_next = term_events_next + strlen(term_events_next);
    }
    return term_parse_event(token, out);
}

static bool term_size_inited = false;
static int term_size_cols = 80;
static int term_size_rows = 24;

static void term_size_init(void) {
    if (term_size_inited) {
        return;
    }
    term_size_inited = true;
    const char* env = getenv("SURGE_TERM_SIZE");
    if (env == NULL || env[0] == 0) {
        return;
    }
    const char* p = env;
    char* end = NULL;
    long cols = strtol(p, &end, 10);
    if (end == p) {
        return;
    }
    char sep = *end;
    if (sep != 'x' && sep != 'X' && sep != ',') {
        return;
    }
    p = end + 1;
    long rows = strtol(p, &end, 10);
    if (end == p) {
        return;
    }
    term_size_cols = (int)cols;
    term_size_rows = (int)rows;
}

static void term_get_size(int* cols, int* rows) {
    if (cols == NULL || rows == NULL) {
        return;
    }
    term_size_init();
    *cols = term_size_cols;
    *rows = term_size_rows;
}

void rt_term_enter_alt_screen(void) {
}

void rt_term_exit_alt_screen(void) {
}

void rt_term_set_raw_mode(bool enabled) {
    (void)enabled;
}

void rt_term_hide_cursor(void) {
}

void rt_term_show_cursor(void) {
}

void* rt_term_size(void) {
    int cols = 80;
    int rows = 24;
    term_get_size(&cols, &rows);
    typedef struct TermSize {
        void* cols;
        void* rows;
    } TermSize;
    TermSize* out = (TermSize*)rt_alloc((uint64_t)sizeof(TermSize), (uint64_t)alignof(TermSize));
    if (out == NULL) {
        const char* msg = "term_size allocation failed";
        rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
        return NULL;
    }
    out->cols = rt_bigint_from_i64((int64_t)cols);
    out->rows = rt_bigint_from_i64((int64_t)rows);
    return (void*)out;
}

void rt_term_write(void* bytes) {
    if (bytes == NULL) {
        return;
    }
    const SurgeArrayHeader* header = (const SurgeArrayHeader*)bytes;
    uint64_t cap = header->cap;
    (void)cap;
    if (header->data == NULL || header->len == 0) {
        return;
    }
    rt_write_stdout((const uint8_t*)header->data, header->len);
}

void rt_term_flush(void) {
}

void* rt_term_read_event(void) {
    TermEventSpec spec = {0};
    if (!term_next_event(&spec)) {
        spec.kind = TERM_EVENT_KIND_EOF;
    }
    void* ev = NULL;
    switch (spec.kind) {
        case TERM_EVENT_KIND_KEY:
            ev = term_make_event_key(spec.key, spec.mods);
            break;
        case TERM_EVENT_KIND_RESIZE:
            ev = term_make_event_resize(spec.cols, spec.rows);
            break;
        case TERM_EVENT_KIND_EOF:
        default:
            ev = term_make_event_eof();
            break;
    }
    if (ev == NULL) {
        const char* msg = "term_read_event allocation failed";
        rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
    }
    return ev;
}
