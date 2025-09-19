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
#include "disasm.h"
#include "sbc.h"
#include "vm.h"

static void usage(const char *prog) {
    fprintf(stderr,
        "Surge v%d.%d.%d\n"
        "Usage:\n"
        "  %s [--shadow DENY|ALLOW|CONTROLLED] tokenize <file | ->\n"
        "  %s [--shadow DENY|ALLOW|CONTROLLED] ast       <file | ->\n"
        "  %s [--shadow DENY|ALLOW|CONTROLLED] diag      <file | ->\n"
        "  %s [--shadow DENY|ALLOW|CONTROLLED] sema      <file | ->\n"
        "  %s disasm <file.sbc>\n"
        "  %s runbc [--trace-vm] <file.sbc>\n",
        SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH,
        prog, prog, prog, prog, prog, prog
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

static int cmd_sema(const char *path, ShadowPolicy shadow) {
    // прокинем политику внутрь
    // (чуть перепишем cmd_sema, чтобы принять shadow)
    SurgeLexer lx = (SurgeLexer){0};
    if (!init_lexer_from_arg(&lx, path)) {
        fprintf(stderr, "Failed to initialize lexer for %s\n", path);
        return 1;
    }
    surge_diag_set_source(lx.file ? lx.file : path, lx.buf, lx.len);

    SurgeParser ps; parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);

    Sema sema; sema_init(&sema);
    sema.shadow = shadow;
    bool ok = sema_check_unit(&sema, unit);
    if (ok) { printf("OK\n"); }

    sema_destroy(&sema);
    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);
    return ok? 0 : 1;
}

static int cmd_disasm_file(const char *path) {
    return surge_disasm_file(path, stdout);
}

static void vm_print_return_value(const VmValue *value) {
    if (!value) {
        printf("exit=null\n");
        return;
    }
    switch (value->tag) {
        case VM_VT_NULL:
            printf("exit=null\n");
            break;
        case VM_VT_BOOL:
            printf("exit=%s\n", value->as.b ? "true" : "false");
            break;
        case VM_VT_I64:
            printf("exit=%lld\n", (long long)value->as.i64);
            break;
        case VM_VT_F64:
            printf("exit=%g\n", value->as.f64);
            break;
        case VM_VT_STR: {
            printf("exit=\"");
            if (value->as.str.data && value->as.str.len) {
                fwrite(value->as.str.data, 1, value->as.str.len, stdout);
            }
            printf("\"\n");
            break;
        }
        case VM_VT_ARR:
            printf("exit=<array>\n");
            break;
        default:
            printf("exit=<unknown>\n");
            break;
    }
}

static int cmd_runbc(const char *path, bool trace_vm) {
    SbcImage img;
    if (!sbc_load_from_file(path, &img)) {
        fprintf(stderr, "vm: failed to load %s\n", path);
        return 1;
    }

    VmConfig cfg;
    vm_config_defaults(&cfg);
    cfg.trace = trace_vm;

    Vm vm;
    if (!vm_init(&vm, &cfg)) {
        fprintf(stderr, "vm: failed to initialize VM\n");
        sbc_unload(&img);
        return 1;
    }

    VmRunResult result;
    memset(&result, 0, sizeof(result));
    VmRunStatus status = vm_run_main(&vm, &img, &result);

    int rc = 0;
    if (status == VM_RUN_OK) {
        vm_print_return_value(&result.return_value);
    } else {
        const char *err = vm_last_error(&vm);
        if (!err) {
            err = (status == VM_RUN_TRAP)
                ? "vm: execution trapped"
                : "vm: execution failed";
        }
        fprintf(stderr, "%s\n", err);
        rc = (status == VM_RUN_TRAP) ? 2 : 1;
    }

    vm_value_release(&vm, &result.return_value);
    vm_destroy(&vm);
    sbc_unload(&img);
    return rc;
}

static ShadowPolicy parse_shadow(const char *s){
    if (!s) return SHADOW_DENY;
    if (strcmp(s,"DENY")==0) return SHADOW_DENY;
    if (strcmp(s,"ALLOW")==0) return SHADOW_ALLOW;
    if (strcmp(s,"CONTROLLED")==0) return SHADOW_CONTROLLED;
    return SHADOW_DENY;
}

/* ---- main ---- */

int main(int argc, char **argv) {
    if (argc < 3) {
        usage(argv[0]);
        return 2;
    }

    int argi = 1;
    ShadowPolicy shadow = SHADOW_DENY;
    if (argi + 2 <= argc && strcmp(argv[argi], "--shadow") == 0) {
        shadow = parse_shadow(argv[argi+1]);
        argi += 2;
    }
    if (argc - argi < 1) { usage(argv[0]); return 2; }

    const char *cmd = argv[argi];

    if (strcmp(cmd, "tokenize") == 0 || strcmp(cmd, "ast") == 0 ||
        strcmp(cmd, "diag") == 0 || strcmp(cmd, "sema") == 0) {
        if (argc - argi < 2) { usage(argv[0]); return 2; }
        const char *path = argv[argi + 1];
        if (strcmp(cmd, "tokenize") == 0) {
            return cmd_tokenize(path);
        } else if (strcmp(cmd, "ast") == 0) {
            return cmd_ast(path);
        } else if (strcmp(cmd, "diag") == 0) {
            return cmd_diag(path);
        } else {
            return cmd_sema(path, shadow);
        }
    } else if (strcmp(cmd, "disasm") == 0) {
        if (argc - argi < 2) { usage(argv[0]); return 2; }
        const char *path = argv[argi + 1];
        return cmd_disasm_file(path);
    } else if (strcmp(cmd, "runbc") == 0) {
        if (argc - argi < 2) { usage(argv[0]); return 2; }
        int next = argi + 1;
        bool trace_vm = false;
        if (next < argc && strcmp(argv[next], "--trace-vm") == 0) {
            trace_vm = true;
            ++next;
        }
        if (next >= argc) { usage(argv[0]); return 2; }
        const char *path = argv[next];
        return cmd_runbc(path, trace_vm);
    } else {
        usage(argv[0]);
        return 2;
    }
}
