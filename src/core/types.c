#include "types.h"
#include <stdlib.h>
#include <string.h>

const SurgeType TY_Invalid = { TY_INVALID, NULL };
const SurgeType TY_Int     = { TY_INT,     NULL };
const SurgeType TY_Float   = { TY_FLOAT,   NULL };
const SurgeType TY_Bool    = { TY_BOOL,    NULL };
const SurgeType TY_String  = { TY_STRING,  NULL };

typedef struct ArrCache { const SurgeType *elem; SurgeType *arr; } ArrCache;
static ArrCache *g_arrs = NULL;
static size_t g_arrs_n = 0, g_arrs_cap = 0;

static SurgeType *new_type(SurgeTypeKind k, const SurgeType *elem) {
    SurgeType *t = (SurgeType*)malloc(sizeof(SurgeType));
    t->kind = k; t->elem = elem; return t;
}

const SurgeType *ty_array_of(const SurgeType *elem) {
    for (size_t i=0;i<g_arrs_n;i++) if (g_arrs[i].elem == elem) return g_arrs[i].arr;
    if (g_arrs_n == g_arrs_cap){
        g_arrs_cap = g_arrs_cap? g_arrs_cap*2 : 8;
        g_arrs = (ArrCache*)realloc(g_arrs, g_arrs_cap * sizeof(*g_arrs));
    }
    SurgeType *t = new_type(TY_ARRAY, elem);
    g_arrs[g_arrs_n++] = (ArrCache){ elem, t };
    return t;
}

bool ty_equal(const SurgeType *a, const SurgeType *b){
    if (a == b) return true;
    if (!a || !b) return false;
    if (a->kind != b->kind) return false;
    if (a->kind == TY_ARRAY) return ty_equal(a->elem, b->elem);
    return true;
}

const char *ty_name(const SurgeType *t){
    if (!t) return "<null>";
    switch (t->kind){
        case TY_INT: return "int";
        case TY_FLOAT: return "float";
        case TY_BOOL: return "bool";
        case TY_STRING: return "string";
        case TY_ARRAY: return "[]";
        default: return "<invalid>";
    }
}