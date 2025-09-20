#ifndef SURGE_SEMA_H
#define SURGE_SEMA_H

#include <stdbool.h>
#include "ast.h"
#include "types.h"

// Символ
typedef enum {
    SYM_VAR,
    SYM_SIGNAL,
    SYM_FN
} SymKind;

// Shadow policy
typedef enum {
    SHADOW_DENY = 0,   // запрещено перекрывать имена из предков
    SHADOW_ALLOW,      // можно перекрывать, запрет только на дубликат в текущем скоупе
    SHADOW_CONTROLLED  // можно перекрывать, но тип должен совпадать
} ShadowPolicy;

typedef struct {
    SymKind kind;
    const SurgeType *type;   // для var/signal/ret type для fn
    bool is_pure;
    bool is_global;
} Symbol;

typedef struct {
    char *name;
    const SurgeType *type;
} TypeAlias;

// Скоп
typedef struct SemaScope SemaScope;
struct SemaScope {
    SemaScope *parent;
    struct Entry { char *name; Symbol sym; SurgeSrcPos pos; } *entries;
    size_t n, cap;
};

// Контекст семантики
typedef struct {
    bool had_error;
    SemaScope *scope;

    // policy
    ShadowPolicy shadow;

    int pure_depth; // >0 when checking inside pure-required context

    const SurgeType *current_ret;
    bool current_fn_has_ret;

    // type aliases (global for MVP)
    TypeAlias *aliases; size_t alias_n, alias_cap;
} Sema;

// API
void sema_init(Sema *sema);
void sema_destroy(Sema *sema);
bool sema_insert(Sema *s, const char *name, Symbol sym, SurgeSrcPos pos);

// Основная проверка: заполняет ошибки через diagnostics
bool sema_check_unit(Sema *sema, SurgeAstUnit *unit);

#endif // SURGE_SEMA_H
