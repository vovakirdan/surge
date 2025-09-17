#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>

#include "config.h"
#include "lexer.h"
#include "parser.h"
#include "ast.h"
#include "token.h"
#include "diagnostics.h"
#include "sema.h"

static void usage(const char *prog) {
    fprintf(stderr,
        "Surge v%d.%d.%d\n"
        "Usage:\n"
        "  %s tokenize <file | ->   # PhaseA pt.1 — print tokens\n"
        "  %s ast       <file | ->   # PhaseA pt.2 — print AST snapshot\n"
        "  %s diag      <file | ->   # PhaseA pt.3 — parse and show diagnostics\n"
        "  %s sema      <file | ->   # Phase B pt.1 — semantic check (prints OK or errors)\n",
        SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH,
        prog, prog, prog, prog
    );
}

/* ---- helpers to load source into lexer ---- */

static int init_lexer_from_arg(SurgeLexer *lx, const char *arg_path) {
    if (strcmp(arg_path, "-") == 0) {
        size_t cap = 4096, len = 0;
        char *buf = (char*)malloc(cap);
        if (!buf) { fprintf(stderr, "OOM\n"); return 0; }
        int ch;
        while ((ch = fgetc(stdin)) != EOF) {
            if (len + 1 >= cap) {
                cap *= 2;
                char *nb = (char*)realloc(buf, cap);
                if (!nb) { free(buf); fprintf(stderr, "OOM\n"); return 0; }
                buf = nb;
            }
            buf[len++] = (char)ch;
        }
        buf[len] = '\0';
        int ok = surge_lexer_init_from_string(lx, buf, "<stdin>");
        free(buf);
        return ok;
    } else {
        return surge_lexer_init_from_file(lx, arg_path);
    }
}

/* ---- tokenize subcommand ---- */

static void print_token(const SurgeToken *t) {
    const char *k = surge_token_kind_cstr(t->kind);
    switch (t->kind) {
        case TOK_INT:
            printf("Int(%lld)\n", (long long)t->int_value);
            break;
        case TOK_FLOAT:
            printf("Float(%g)\n", t->float_value);
            break;
        case TOK_STRING:
            printf("String(\"%s\")\n", t->lexeme ? t->lexeme : "");
            break;
        case TOK_IDENTIFIER:
            printf("Ident(%s)\n", t->lexeme ? t->lexeme : "");
            break;
        case TOK_KW_TRUE:
            printf("Bool(true)\n"); break;
        case TOK_KW_FALSE:
            printf("Bool(false)\n"); break;
        case TOK_EOF:
            printf("EOF\n"); break;
        default:
            /* generic token */
            printf("%s\n", k ? k : "TOKEN");
            break;
    }
}

static int cmd_tokenize(const char *path) {
    SurgeLexer lx = {0};
    if (!init_lexer_from_arg(&lx, path)) {
        fprintf(stderr, "Failed to initialize lexer for %s\n", path);
        return 1;
    }

    surge_diag_set_source(lx.file ? lx.file : path, lx.buf, lx.len);

    for (;;) {
        SurgeToken t = surge_lexer_next(&lx);
        print_token(&t);
        if (t.kind == TOK_EOF) {
            surge_token_free(&t);
            break;
        }
        surge_token_free(&t);
    }

    surge_lexer_destroy(&lx);
    return 0;
}

/* ---- ast subcommand ---- */

static int cmd_ast(const char *path) {
    SurgeLexer lx = {0};
    if (!init_lexer_from_arg(&lx, path)) {
        fprintf(stderr, "Failed to initialize lexer for %s\n", path);
        return 1;
    }

    surge_diag_set_source(lx.file ? lx.file : path, lx.buf, lx.len);

    SurgeParser ps;
    parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);
    ast_print_unit(unit, stdout);

    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);
    return 0;
}

/* ---- diag subcommand ----
   Просто парсим, не печатаем AST; ошибки уйдут в stderr с контекстом.
   Если ошибок нет — ничего не выводим (успех). */

static int cmd_diag(const char *path) {
    SurgeLexer lx = {0};
    if (!init_lexer_from_arg(&lx, path)) {
        fprintf(stderr, "Failed to initialize lexer for %s\n", path);
        return 1;
    }

    surge_diag_set_source(lx.file ? lx.file : path, lx.buf, lx.len);

    SurgeParser ps;
    parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);
    (void)unit; // не печатаем дерево в diag-режиме

    int had_error = ps.had_error ? 1 : 0;

    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);

    return had_error;
}

/* ---- sema subcommand ---- 
   Просто семантическую проверку, не печатаем AST; ошибки уйдут в stderr с контекстом.
   Если ошибок нет — ничего не выводим (успех). */

static int cmd_sema(const char *path) {
    SurgeLexer lx = {0};
    if (!init_lexer_from_arg(&lx, path)) {
        fprintf(stderr, "Failed to initialize lexer for %s\n", path);
        return 1;
    }
    surge_diag_set_source(lx.file ? lx.file : path, lx.buf, lx.len);

    SurgeParser ps; parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);

    Sema sema; sema_init(&sema);
    bool ok = sema_check_unit(&sema, unit);
    if (ok) { printf("OK\n"); }

    sema_destroy(&sema);
    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);
    return ok? 0 : 1;
}


/* ---- main ---- */

int main(int argc, char **argv) {
    if (argc < 3) {
        usage(argv[0]);
        return 2;
    }

    const char *cmd  = argv[1];
    const char *path = argv[2];

    if (strcmp(cmd, "tokenize") == 0) {
        return cmd_tokenize(path);
    } else if (strcmp(cmd, "ast") == 0) {
        return cmd_ast(path);
    } else if (strcmp(cmd, "diag") == 0) {
        return cmd_diag(path);
    } else if (strcmp(cmd, "sema") == 0) {
        return cmd_sema(path);
    } else {
        usage(argv[0]);
        return 2;
    }
}
