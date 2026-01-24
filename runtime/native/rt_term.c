#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <ctype.h>
#include <errno.h>
#include <signal.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/select.h>
#include <termios.h>
#include <unistd.h>

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

enum {
    TERM_MOD_SHIFT = 1,
    TERM_MOD_ALT = 2,
    TERM_MOD_CTRL = 4,
    TERM_MOD_META = 8,
};

typedef struct TermKeyEventPayload {
    void* key;
    uint8_t mods;
} TermKeyEventPayload;

typedef struct TermKeyEvent {
    void* key;
    uint8_t mods;
} TermKeyEvent;

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

static bool term_raw_enabled = false;
static bool term_orig_valid = false;
static struct termios term_orig;
static bool term_exit_handler_installed = false;

static volatile sig_atomic_t term_sigwinch = 0;
static bool term_sigwinch_installed = false;
static struct sigaction term_prev_sigwinch;

static bool term_events_override = false;
static bool term_size_override = false;
static bool term_debug_enabled_flag = false;
static bool term_debug_inited = false;

static bool term_debug_enabled(void) {
    if (term_debug_inited) {
        return term_debug_enabled_flag;
    }
    term_debug_inited = true;
    const char* env = getenv("SURGE_TERM_DEBUG");
    if (env == NULL || env[0] == '\0' || (env[0] == '0' && env[1] == '\0')) {
        term_debug_enabled_flag = false;
        return false;
    }
    term_debug_enabled_flag = true;
    return true;
}

static void term_debug_printf(const char* fmt, ...) {
    if (!term_debug_enabled() || fmt == NULL) {
        return;
    }
    char buf[256];
    va_list args;
    va_start(args, fmt);
    int n = vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    if (n <= 0) {
        return;
    }
    uint64_t len = (uint64_t)n;
    if ((size_t)n >= sizeof(buf)) {
        len = (uint64_t)(sizeof(buf) - 1);
    }
    rt_write_stderr((const uint8_t*)buf, len);
}

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

static void* term_make_key_event(TermKeyData key, uint8_t mods) {
    TermKeyEvent* ev =
        (TermKeyEvent*)rt_alloc((uint64_t)sizeof(TermKeyEvent), (uint64_t)alignof(TermKeyEvent));
    if (ev == NULL) {
        return NULL;
    }
    ev->key = term_make_key(key);
    ev->mods = mods;
    term_debug_printf(
        "term_make_key_event ev=%p key=%p mods=%u\n", (void*)ev, ev->key, (unsigned)mods);
    return (void*)ev;
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
    void* key_event = term_make_key_event(key, mods);
    if (key_event == NULL) {
        return NULL;
    }
    if (term_debug_enabled()) {
        TermKeyEvent* ev = (TermKeyEvent*)key_event;
        uint32_t key_tag = 0;
        if (ev->key != NULL) {
            key_tag = *(uint32_t*)ev->key;
        }
        term_debug_printf(
            "term_make_event_key tag=%u ev=%p key_event=%p key=%p key_tag=%u mods=%u\n",
            TERM_EVENT_TAG_KEY,
            (void*)mem,
            key_event,
            ev->key,
            (unsigned)key_tag,
            (unsigned)mods);
    }
    *(void**)(mem + payload_offset) = key_event;
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
    term_events_override = true;
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

static int term_tty_fd(void) {
    if (isatty(STDOUT_FILENO)) {
        return STDOUT_FILENO;
    }
    if (isatty(STDIN_FILENO)) {
        return STDIN_FILENO;
    }
    return -1;
}

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
    term_size_override = true;
}

static void term_get_size(int* cols, int* rows) {
    if (cols == NULL || rows == NULL) {
        return;
    }
    term_size_init();
    if (!term_size_override) {
        int fd = term_tty_fd();
        if (fd >= 0) {
            struct winsize ws = {0};
            if (ioctl(fd, TIOCGWINSZ, &ws) == 0) {
                if (ws.ws_col > 0) {
                    term_size_cols = (int)ws.ws_col;
                }
                if (ws.ws_row > 0) {
                    term_size_rows = (int)ws.ws_row;
                }
            }
        }
    }
    *cols = term_size_cols;
    *rows = term_size_rows;
}

static void term_handle_sigwinch(int signo) {
    (void)signo;
    term_sigwinch = 1;
}

static void term_install_sigwinch(void) {
#ifdef SIGWINCH
    if (term_sigwinch_installed) {
        return;
    }
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = term_handle_sigwinch;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = SA_RESTART;
    if (sigaction(SIGWINCH, &sa, &term_prev_sigwinch) == 0) {
        term_sigwinch_installed = true;
    }
#else
    (void)term_handle_sigwinch;
#endif
}

static void term_restore_sigwinch(void) {
#ifdef SIGWINCH
    if (!term_sigwinch_installed) {
        return;
    }
    sigaction(SIGWINCH, &term_prev_sigwinch, NULL);
    term_sigwinch_installed = false;
#endif
}

static void term_write_ansi(const char* seq) {
    if (seq == NULL) {
        return;
    }
    rt_write_stdout((const uint8_t*)seq, (uint64_t)strlen(seq));
}

static void term_restore_at_exit(void) {
    rt_term_set_raw_mode(false);
    rt_term_show_cursor();
    rt_term_exit_alt_screen();
}

void rt_term_enter_alt_screen(void) {
    if (term_tty_fd() < 0) {
        return;
    }
    term_write_ansi("\x1b[?1049h");
    term_write_ansi("\x1b[2J\x1b[H");
}

void rt_term_exit_alt_screen(void) {
    if (term_tty_fd() < 0) {
        return;
    }
    term_write_ansi("\x1b[?1049l");
}

void rt_term_set_raw_mode(bool enabled) {
    int fd = term_tty_fd();
    if (fd < 0) {
        return;
    }
    if (enabled) {
        if (!term_orig_valid) {
            if (tcgetattr(fd, &term_orig) != 0) {
                return;
            }
            term_orig_valid = true;
        }
        if (!term_raw_enabled) {
            struct termios raw = term_orig;
            raw.c_iflag &= (tcflag_t) ~(BRKINT | ICRNL | INPCK | ISTRIP | IXON);
            raw.c_oflag &= (tcflag_t) ~(OPOST);
            raw.c_cflag |= (tcflag_t)CS8;
            raw.c_lflag &= (tcflag_t) ~(ECHO | ICANON | IEXTEN | ISIG);
            raw.c_cc[VMIN] = 1;
            raw.c_cc[VTIME] = 0;
            if (tcsetattr(fd, TCSAFLUSH, &raw) == 0) {
                term_raw_enabled = true;
                term_install_sigwinch();
                if (!term_exit_handler_installed) {
                    atexit(term_restore_at_exit);
                    term_exit_handler_installed = true;
                }
            }
        }
        return;
    }
    if (term_raw_enabled && term_orig_valid) {
        tcsetattr(fd, TCSAFLUSH, &term_orig);
        term_raw_enabled = false;
    }
    term_restore_sigwinch();
}

void rt_term_hide_cursor(void) {
    if (term_tty_fd() < 0) {
        return;
    }
    term_write_ansi("\x1b[?25l");
}

void rt_term_show_cursor(void) {
    if (term_tty_fd() < 0) {
        return;
    }
    term_write_ansi("\x1b[?25h");
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
    int fd = term_tty_fd();
    if (fd >= 0) {
        tcdrain(fd);
    }
}

static uint8_t term_mods_from_xterm(int mod) {
    switch (mod) {
        case 2:
            return TERM_MOD_SHIFT;
        case 3:
            return TERM_MOD_ALT;
        case 4:
            return TERM_MOD_SHIFT | TERM_MOD_ALT;
        case 5:
            return TERM_MOD_CTRL;
        case 6:
            return TERM_MOD_SHIFT | TERM_MOD_CTRL;
        case 7:
            return TERM_MOD_ALT | TERM_MOD_CTRL;
        case 8:
            return TERM_MOD_SHIFT | TERM_MOD_ALT | TERM_MOD_CTRL;
        default:
            return 0;
    }
}

static bool
term_set_key_event(TermEventSpec* out, TermKeyKind kind, uint32_t ch, uint8_t f, uint8_t mods) {
    if (out == NULL) {
        return false;
    }
    out->kind = TERM_EVENT_KIND_KEY;
    out->mods = mods;
    out->key.kind = kind;
    out->key.ch = ch;
    out->key.f = f;
    return true;
}

static bool term_set_resize_event(TermEventSpec* out) {
    if (out == NULL) {
        return false;
    }
    int cols = 80;
    int rows = 24;
    term_get_size(&cols, &rows);
    out->kind = TERM_EVENT_KIND_RESIZE;
    out->cols = (int64_t)cols;
    out->rows = (int64_t)rows;
    return true;
}

static int term_read_byte_blocking(void) {
    uint8_t ch = 0;
    while (true) {
        ssize_t n = read(STDIN_FILENO, &ch, 1);
        if (n == 1) {
            return (int)ch;
        }
        if (n == 0) {
            return -1;
        }
        if (errno == EINTR) {
            if (term_sigwinch != 0) {
                return -2;
            }
            continue;
        }
        return -1;
    }
}

static int term_read_byte_timeout(int timeout_ms) {
    fd_set set;
    FD_ZERO(&set);
    FD_SET(STDIN_FILENO, &set);
    struct timeval tv;
    tv.tv_sec = timeout_ms / 1000;
    tv.tv_usec = (timeout_ms % 1000) * 1000;
    int res = select(STDIN_FILENO + 1, &set, NULL, NULL, &tv);
    if (res <= 0) {
        return -1;
    }
    return term_read_byte_blocking();
}

static bool term_read_utf8(uint8_t first, uint32_t* out) {
    if (out == NULL) {
        return false;
    }
    if (first < 0x80) {
        *out = first;
        return true;
    }
    uint32_t code = 0;
    int need = 0;
    if ((first & 0xE0) == 0xC0) {
        code = (uint32_t)(first & 0x1F);
        need = 1;
    } else if ((first & 0xF0) == 0xE0) {
        code = (uint32_t)(first & 0x0F);
        need = 2;
    } else if ((first & 0xF8) == 0xF0) {
        code = (uint32_t)(first & 0x07);
        need = 3;
    } else {
        return false;
    }
    for (int i = 0; i < need; i++) {
        int b = term_read_byte_blocking();
        if (b < 0) {
            return false;
        }
        if ((b & 0xC0) != 0x80) {
            return false;
        }
        code = (code << 6) | (uint32_t)(b & 0x3F);
    }
    *out = code;
    return true;
}

static bool term_parse_csi(TermEventSpec* out) {
    int params[3] = {0, 0, 0};
    int pcount = 0;
    int current = 0;
    bool have_num = false;
    while (true) {
        int b = term_read_byte_blocking();
        if (b < 0) {
            return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
        }
        if (b >= '0' && b <= '9') {
            have_num = true;
            current = current * 10 + (b - '0');
            continue;
        }
        if (b == ';') {
            if (pcount < 3) {
                params[pcount++] = have_num ? current : 0;
            }
            current = 0;
            have_num = false;
            continue;
        }
        if (have_num || pcount > 0) {
            if (pcount < 3) {
                params[pcount++] = have_num ? current : 0;
            }
        }
        int mod_param = 0;
        if (pcount >= 2) {
            mod_param = params[1];
        }
        uint8_t mods = term_mods_from_xterm(mod_param);
        switch (b) {
            case 'A':
                return term_set_key_event(out, TERM_KEY_KIND_UP, 0, 0, mods);
            case 'B':
                return term_set_key_event(out, TERM_KEY_KIND_DOWN, 0, 0, mods);
            case 'C':
                return term_set_key_event(out, TERM_KEY_KIND_RIGHT, 0, 0, mods);
            case 'D':
                return term_set_key_event(out, TERM_KEY_KIND_LEFT, 0, 0, mods);
            case 'H':
                return term_set_key_event(out, TERM_KEY_KIND_HOME, 0, 0, mods);
            case 'F':
                return term_set_key_event(out, TERM_KEY_KIND_END, 0, 0, mods);
            case 'Z':
                return term_set_key_event(out, TERM_KEY_KIND_TAB, 0, 0, TERM_MOD_SHIFT);
            case '~': {
                int key = (pcount >= 1) ? params[0] : 0;
                uint8_t f = 0;
                switch (key) {
                    case 1:
                    case 7:
                        return term_set_key_event(out, TERM_KEY_KIND_HOME, 0, 0, mods);
                    case 4:
                    case 8:
                        return term_set_key_event(out, TERM_KEY_KIND_END, 0, 0, mods);
                    case 3:
                        return term_set_key_event(out, TERM_KEY_KIND_DELETE, 0, 0, mods);
                    case 5:
                        return term_set_key_event(out, TERM_KEY_KIND_PAGE_UP, 0, 0, mods);
                    case 6:
                        return term_set_key_event(out, TERM_KEY_KIND_PAGE_DOWN, 0, 0, mods);
                    case 11:
                        f = 1;
                        break;
                    case 12:
                        f = 2;
                        break;
                    case 13:
                        f = 3;
                        break;
                    case 14:
                        f = 4;
                        break;
                    case 15:
                        f = 5;
                        break;
                    case 17:
                        f = 6;
                        break;
                    case 18:
                        f = 7;
                        break;
                    case 19:
                        f = 8;
                        break;
                    case 20:
                        f = 9;
                        break;
                    case 21:
                        f = 10;
                        break;
                    case 23:
                        f = 11;
                        break;
                    case 24:
                        f = 12;
                        break;
                    default:
                        break;
                }
                if (f != 0) {
                    return term_set_key_event(out, TERM_KEY_KIND_F, 0, f, mods);
                }
                return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
            }
            default:
                break;
        }
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
}

static bool term_parse_ss3(TermEventSpec* out) {
    int b = term_read_byte_blocking();
    if (b < 0) {
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
    switch (b) {
        case 'A':
            return term_set_key_event(out, TERM_KEY_KIND_UP, 0, 0, 0);
        case 'B':
            return term_set_key_event(out, TERM_KEY_KIND_DOWN, 0, 0, 0);
        case 'C':
            return term_set_key_event(out, TERM_KEY_KIND_RIGHT, 0, 0, 0);
        case 'D':
            return term_set_key_event(out, TERM_KEY_KIND_LEFT, 0, 0, 0);
        case 'H':
            return term_set_key_event(out, TERM_KEY_KIND_HOME, 0, 0, 0);
        case 'F':
            return term_set_key_event(out, TERM_KEY_KIND_END, 0, 0, 0);
        case 'P':
            return term_set_key_event(out, TERM_KEY_KIND_F, 0, 1, 0);
        case 'Q':
            return term_set_key_event(out, TERM_KEY_KIND_F, 0, 2, 0);
        case 'R':
            return term_set_key_event(out, TERM_KEY_KIND_F, 0, 3, 0);
        case 'S':
            return term_set_key_event(out, TERM_KEY_KIND_F, 0, 4, 0);
        default:
            break;
    }
    return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
}

static bool term_read_escape_sequence(TermEventSpec* out) {
    int next = term_read_byte_timeout(15);
    if (next < 0) {
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
    if (next == '[') {
        return term_parse_csi(out);
    }
    if (next == 'O') {
        return term_parse_ss3(out);
    }
    if (next == 0x1B) {
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
    uint32_t ch = 0;
    if (!term_read_utf8((uint8_t)next, &ch)) {
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
    return term_set_key_event(out, TERM_KEY_KIND_CHAR, ch, 0, TERM_MOD_ALT);
}

static bool term_read_key_event(TermEventSpec* out) {
    int b = term_read_byte_blocking();
    if (b == -2) {
        term_sigwinch = 0;
        return term_set_resize_event(out);
    }
    if (b < 0) {
        out->kind = TERM_EVENT_KIND_EOF;
        return true;
    }
    if (b == 0x1B) {
        return term_read_escape_sequence(out);
    }
    if (b == '\r' || b == '\n') {
        return term_set_key_event(out, TERM_KEY_KIND_ENTER, 0, 0, 0);
    }
    if (b == '\t') {
        return term_set_key_event(out, TERM_KEY_KIND_TAB, 0, 0, 0);
    }
    if (b == 0x7F || b == 0x08) {
        return term_set_key_event(out, TERM_KEY_KIND_BACKSPACE, 0, 0, 0);
    }
    if (b >= 0x01 && b <= 0x1A) {
        uint32_t ch = (uint32_t)('a' + (b - 1));
        return term_set_key_event(out, TERM_KEY_KIND_CHAR, ch, 0, TERM_MOD_CTRL);
    }
    if (b == 0x00) {
        return term_set_key_event(out, TERM_KEY_KIND_CHAR, (uint32_t)'@', 0, TERM_MOD_CTRL);
    }
    uint32_t ch = 0;
    if (!term_read_utf8((uint8_t)b, &ch)) {
        return term_set_key_event(out, TERM_KEY_KIND_ESC, 0, 0, 0);
    }
    return term_set_key_event(out, TERM_KEY_KIND_CHAR, ch, 0, 0);
}

static bool term_read_event_spec(TermEventSpec* out) {
    if (out == NULL) {
        return false;
    }
    term_events_init();
    if (term_events_override) {
        return term_next_event(out);
    }
    if (term_sigwinch != 0) {
        term_sigwinch = 0;
        return term_set_resize_event(out);
    }
    return term_read_key_event(out);
}

void* rt_term_read_event(void) {
    TermEventSpec spec = {0};
    if (!term_read_event_spec(&spec)) {
        spec.kind = TERM_EVENT_KIND_EOF;
    }
    if (term_debug_enabled()) {
        term_debug_printf("term_read_event spec kind=%d key_kind=%d mods=%u cols=%lld rows=%lld\n",
                          (int)spec.kind,
                          (int)spec.key.kind,
                          (unsigned)spec.mods,
                          (long long)spec.cols,
                          (long long)spec.rows);
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
    if (term_debug_enabled()) {
        uint32_t tag = 0;
        if (ev != NULL) {
            tag = *(uint32_t*)ev;
        }
        term_debug_printf("term_read_event result=%p tag=%u\n", ev, (unsigned)tag);
    }
    if (ev == NULL) {
        const char* msg = "term_read_event allocation failed";
        rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
    }
    return ev;
}
