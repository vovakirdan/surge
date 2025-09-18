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

typedef struct {
    SymKind kind;
    const SurgeType *type;   // для var/signal/ret type для fn
    // для fn можно позже добавить сигнатуры (параметры)
} Symbol;

typedef struct {
    char *name;
    const SurgeType *type;
} TypeAlias;

// Скоп
typedef struct SemaScope SemaScope;
struct SemaScope {
    SemaScope *parent;
    struct Entry { char *name; Symbol sym; } *entries;
    size_t n, cap;
};

// Контекст семантики
typedef struct {
    bool had_error;
    SemaScope *scope;

    // type aliases (global for MVP)
    TypeAlias *aliases; size_t alias_n, alias_cap;
} Sema;

// API
void sema_init(Sema *sema);
void sema_destroy(Sema *sema);

// Основная проверка: заполняет ошибки через diagnostics
bool sema_check_unit(Sema *sema, SurgeAstUnit *unit);

#endif // SURGE_SEMA_H
