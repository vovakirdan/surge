#include "lexer.h"
#include "config.h"
#include "diagnostics.h"

#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <stdio.h>
#include <errno.h>

// ---------- Internal helpers ----------

static inline int lx_eof(const SurgeLexer *lx) {
    return lx->idx >= lx->len;
}
static inline char lx_peek(const SurgeLexer *lx) {
    return lx->idx < lx->len ? lx->buf[lx->idx] : '\0';
}
static inline char lx_peek2(const SurgeLexer *lx) {
    return (lx->idx + 1) < lx->len ? lx->buf[lx->idx + 1] : '\0';
}
static inline char lx_advance(SurgeLexer *lx) {
    if (lx->idx >= lx->len) return '\0';
    char c = lx->buf[lx->idx++];
    if (c == '\n') { lx->line++; lx->col = 1; }
    else { lx->col++; }
    return c;
}
static char *xstrndup0(const char *s, size_t n) {
    char *p = (char*)malloc(n + 1);
    if (!p) return NULL;
    memcpy(p, s, n);
    p[n] = '\0';
    return p;
}
static int is_ident_start(char c) {
    return isalpha((unsigned char)c) || c == '_';
}
static int is_ident_part(char c) {
    return isalnum((unsigned char)c) || c == '_';
}

static void skip_bom(SurgeLexer *lx) {
    if (lx->len >= 3 &&
        (unsigned char)lx->buf[0] == 0xEF &&
        (unsigned char)lx->buf[1] == 0xBB &&
        (unsigned char)lx->buf[2] == 0xBF) {
        lx->idx = 3; lx->col += 3;
    }
}

static void skip_ws_and_comments(SurgeLexer *lx) {
    for (;;) {
        char c = lx_peek(lx);
        // whitespace
        if (c==' '||c=='\t'||c=='\r'||c=='\n') { lx_advance(lx); continue; }
        // // line comment
        if (c=='/' && lx_peek2(lx)=='/') {
            lx_advance(lx); lx_advance(lx);
            while (!lx_eof(lx) && lx_peek(lx)!='\n') lx_advance(lx);
            continue;
        }
        // /* block comment */
        if (c=='/' && lx_peek2(lx)=='*') {
            lx_advance(lx); lx_advance(lx);
            while (!lx_eof(lx)) {
                if (lx_peek(lx)=='*' && lx_peek2(lx)=='/') { lx_advance(lx); lx_advance(lx); break; }
                lx_advance(lx);
            }
            continue;
        }
        break;
    }
}

static SurgeToken make_simple(SurgeTokenKind k, SurgeSrcPos pos, const char *lex, size_t n) {
    SurgeToken t = (SurgeToken){0};
    t.kind = k;
    t.pos  = pos;
    t.lexeme = xstrndup0(lex, n);
    t.length = n;
    return t;
}

static SurgeToken make_error(SurgeLexer *lx, SurgeSrcPos pos, const char *msg) {
    SurgeToken t = {0};
    t.kind = TOK_ERROR;
    t.pos  = pos;
    t.lexeme = xstrndup0(msg, strlen(msg));
    t.length = strlen(msg);
    lx->had_error = true;
    if (!t.lexeme) surge_diag_errorf(pos, "Out of memory (error message)");
    return t;
}

static SurgeToken lex_number(SurgeLexer *lx, SurgeSrcPos pos) {
    size_t begin = lx->idx;
    int saw_dot = 0;
    int saw_exp = 0;

    // integer part
    while (isdigit((unsigned char)lx_peek(lx))) lx_advance(lx);
    // fractional part
    if (lx_peek(lx)=='.' && isdigit((unsigned char)lx_peek2(lx))) {
        saw_dot = 1; lx_advance(lx);
        while (isdigit((unsigned char)lx_peek(lx))) lx_advance(lx);
    }
    // exponent part
    if (lx_peek(lx)=='e' || lx_peek(lx)=='E') {
        char n = lx_peek2(lx);
        if (isdigit((unsigned char)n) || n=='+' || n=='-') {
            saw_exp = 1; lx_advance(lx);
            if (lx_peek(lx)=='+'||lx_peek(lx)=='-') lx_advance(lx);
            if (!isdigit((unsigned char)lx_peek(lx))) return make_error(lx, pos, "Invalid float exponent");
            while (isdigit((unsigned char)lx_peek(lx))) lx_advance(lx);
        }
    }

    size_t n = lx->idx - begin;
    char *lex = xstrndup0(&lx->buf[begin], n);
    if (!lex) return make_error(lx, pos, "Out of memory (number)");

    SurgeToken t = {0};
    t.pos = pos;
    t.lexeme = lex;
    t.length = n;

    if (saw_dot || saw_exp) {
        errno = 0;
        char *endp = NULL;
        double v = strtod(lex, &endp);
        if (errno!=0 || endp==lex) {
            return make_error(lx, pos, "Invalid float literal");
        }
        t.kind = TOK_FLOAT;
        t.has_float = true;
        t.float_value = v;
    } else {
        errno = 0;
        char *endp = NULL;
        long long v = strtoll(lex, &endp, 10);
        if (errno!=0 || endp==lex) {
            return make_error(lx, pos, "Invalid integer literal");
        }
        t.kind = TOK_INT;
        t.has_int = true;
        t.int_value = (int64_t)v;
    }
    return t;
}

static SurgeTokenKind keyword_lookup(const char *s, size_t n) {
    // Keep it simple for Phase A (linear checks).
    #define KW(str, tok) if (n==sizeof(str)-1 && strncmp(s, str, sizeof(str)-1)==0) return tok
    KW("let", TOK_KW_LET);
    KW("signal", TOK_KW_SIGNAL);
    KW("fn", TOK_KW_FN);
    KW("if", TOK_KW_IF);
    KW("else", TOK_KW_ELSE);
    KW("while", TOK_KW_WHILE);
    KW("return", TOK_KW_RETURN);
    KW("import", TOK_KW_IMPORT);
    KW("true", TOK_KW_TRUE);
    KW("false", TOK_KW_FALSE);
    KW("parallel", TOK_KW_PARALLEL);
    KW("map", TOK_KW_MAP);
    KW("reduce", TOK_KW_REDUCE);
    KW("own", TOK_KW_OWN);
    KW("channel", TOK_KW_CHANNEL);
    #undef KW
    return TOK_IDENTIFIER;
}

static SurgeToken lex_ident_or_kw(SurgeLexer *lx, SurgeSrcPos pos) {
    size_t begin = lx->idx;
    lx_advance(lx); // consume first validated char
    while (is_ident_part(lx_peek(lx))) lx_advance(lx);
    size_t n = lx->idx - begin;
    SurgeTokenKind k = keyword_lookup(&lx->buf[begin], n);
    return make_simple(k, pos, &lx->buf[begin], n);
}

static SurgeToken lex_string(SurgeLexer *lx, SurgeSrcPos pos) {
    // consume opening "
    lx_advance(lx);
    size_t begin = lx->idx;
    int escaped = 0;
    while (!lx_eof(lx)) {
        char c = lx_peek(lx);
        if (!escaped) {
            if (c == '\\') { escaped = 1; lx_advance(lx); continue; }
            if (c == '"') {
                size_t raw_len = lx->idx - begin;
                lx_advance(lx); // consume closing "
                char *raw = xstrndup0(&lx->buf[begin], raw_len);
                if (!raw) return make_error(lx, pos, "Out of memory (string)");
                SurgeToken t = {0};
                t.kind = TOK_STRING;
                t.pos = pos;
                t.lexeme = raw;
                t.length = raw_len;
                return t;
            }
            lx_advance(lx);
        } else {
            escaped = 0;
            lx_advance(lx);
        }
    }
    return make_error(lx, pos, "Unterminated string literal");
}

static SurgeToken lex_operator_or_punct(SurgeLexer *lx, SurgeSrcPos pos) {
    char c = lx_peek(lx);
    char n = lx_peek2(lx);

    // two-char combos first
    if (c==':' && n=='=') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_BIND, pos, ":=", 2); }
    if (c=='-' && n=='>') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_ARROW, pos, "->", 2); }
    if (c=='=' && n=='=') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_EQ, pos, "==", 2); }
    if (c=='!' && n=='=') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_NE, pos, "!=", 2); }
    if (c=='<' && n=='=') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_LE, pos, "<=", 2); }
    if (c=='>' && n=='=') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_GE, pos, ">=", 2); }
    if (c=='&' && n=='&') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_AND_AND, pos, "&&", 2); }
    if (c=='|' && n=='|') { lx_advance(lx); lx_advance(lx); return make_simple(TOK_OR_OR, pos, "||", 2); }

    switch (c) {
        case '(': lx_advance(lx); return make_simple(TOK_LPAREN, pos, "(", 1);
        case ')': lx_advance(lx); return make_simple(TOK_RPAREN, pos, ")", 1);
        case '{': lx_advance(lx); return make_simple(TOK_LBRACE, pos, "{", 1);
        case '}': lx_advance(lx); return make_simple(TOK_RBRACE, pos, "}", 1);
        case '[': lx_advance(lx); return make_simple(TOK_LBRACKET, pos, "[", 1);
        case ']': lx_advance(lx); return make_simple(TOK_RBRACKET, pos, "]", 1);
        case ',': lx_advance(lx); return make_simple(TOK_COMMA, pos, ",", 1);
        case ';': lx_advance(lx); return make_simple(TOK_SEMICOLON, pos, ";", 1);
        case '.': lx_advance(lx); return make_simple(TOK_DOT, pos, ".", 1);
        case ':': lx_advance(lx); return make_simple(TOK_COLON, pos, ":", 1);
        case '+': lx_advance(lx); return make_simple(TOK_PLUS, pos, "+", 1);
        case '-': lx_advance(lx); return make_simple(TOK_MINUS, pos, "-", 1);
        case '*': lx_advance(lx); return make_simple(TOK_STAR, pos, "*", 1);
        case '/': lx_advance(lx); return make_simple(TOK_SLASH, pos, "/", 1);
        case '%': lx_advance(lx); return make_simple(TOK_PERCENT, pos, "%", 1);
        case '!': lx_advance(lx); return make_simple(TOK_BANG, pos, "!", 1);
        case '=': lx_advance(lx); return make_simple(TOK_ASSIGN, pos, "=", 1);
        case '<': lx_advance(lx); return make_simple(TOK_LT, pos, "<", 1);
        case '>': lx_advance(lx); return make_simple(TOK_GT, pos, ">", 1);
        default: break;
    }

    char msg[64];
    snprintf(msg, sizeof(msg), "Unexpected character '%c' (0x%02X)", c ? c : '?', (unsigned char)c);
    lx_advance(lx);
    return make_error(lx, pos, msg);
}

// ---------- Public API ----------

bool surge_lexer_init_from_string(SurgeLexer *lx, const char *source, const char *file_name) {
    if (!lx || !source) return false;
    memset(lx, 0, sizeof(*lx));
    size_t n = strlen(source);
    lx->buf = (char*)malloc(n + 1);
    if (!lx->buf) return false;
    memcpy(lx->buf, source, n);
    lx->buf[n] = '\0';
    lx->len = n;
    lx->idx = 0;
    lx->line = 1;
    lx->col = 1;
    lx->had_error = false;
    if (file_name) {
        size_t fl = strlen(file_name);
        lx->file = (char*)malloc(fl + 1);
        if (!lx->file) { free(lx->buf); lx->buf = NULL; return false; }
        memcpy(lx->file, file_name, fl + 1);
    }
    skip_bom(lx);
    return true;
}

bool surge_lexer_init_from_file(SurgeLexer *lx, const char *path) {
    if (!lx || !path) return false;
    memset(lx, 0, sizeof(*lx));
    FILE *f = fopen(path, "rb");
    if (!f) return false;
    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return false; }
    long sz = ftell(f);
    if (sz < 0) { fclose(f); return false; }
    rewind(f);

    lx->buf = (char*)malloc((size_t)sz + 1);
    if (!lx->buf) { fclose(f); return false; }
    size_t rd = fread(lx->buf, 1, (size_t)sz, f);
    fclose(f);
    if (rd != (size_t)sz) { free(lx->buf); lx->buf = NULL; return false; }
    lx->buf[sz] = '\0';

    lx->len = (size_t)sz;
    lx->idx = 0;
    lx->line = 1;
    lx->col = 1;
    lx->had_error = false;

    size_t fl = strlen(path);
    lx->file = (char*)malloc(fl + 1);
    if (lx->file) memcpy(lx->file, path, fl + 1);

    skip_bom(lx);
    return true;
}

SurgeToken surge_lexer_next(SurgeLexer *lx) {
    if (!lx) {
        SurgeToken t = {0};
        t.kind = TOK_ERROR;
        t.pos = (SurgeSrcPos){ .file=NULL, .line=0, .col=0 };
        t.lexeme = xstrndup0("Lexer is NULL", 12);
        t.length = 12;
        return t;
    }

    skip_ws_and_comments(lx);

    SurgeSrcPos pos = { .file = lx->file, .line = lx->line, .col = lx->col };
    if (lx_eof(lx)) {
        SurgeToken t = {0};
        t.kind = TOK_EOF;
        t.pos = pos;
        t.lexeme = xstrndup0("EOF", 3);
        t.length = 3;
        return t;
    }

    char c = lx_peek(lx);

    if (is_ident_start(c)) return lex_ident_or_kw(lx, pos);
    if (isdigit((unsigned char)c)) return lex_number(lx, pos);
    if (c == '"') return lex_string(lx, pos);

    return lex_operator_or_punct(lx, pos);
}

void surge_lexer_destroy(SurgeLexer *lx) {
    if (!lx) return;
    if (lx->buf) { free(lx->buf); lx->buf = NULL; }
    if (lx->file) { free(lx->file); lx->file = NULL; }
    lx->len = lx->idx = 0;
    lx->line = lx->col = 0;
    lx->had_error = false;
}
