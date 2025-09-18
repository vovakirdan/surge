#ifndef SURGE_TYPES_H
#define SURGE_TYPES_H

#include <stdbool.h>
#include <stddef.h>

typedef enum {
    TY_INVALID = 0,
    TY_INT, TY_FLOAT, TY_BOOL, TY_STRING,
    TY_ARRAY,       // elem
    TY_REF,         // elem
    TY_OWN,         // elem
    TY_CHANNEL      // elem
} SurgeTypeKind;

typedef struct SurgeType SurgeType;

struct SurgeType {
    SurgeTypeKind kind;
    const SurgeType *elem; // для массивов
};

extern const SurgeType TY_Invalid;
extern const SurgeType TY_Int;
extern const SurgeType TY_Float;
extern const SurgeType TY_Bool;
extern const SurgeType TY_String;

// фабрики
const SurgeType *ty_array_of(const SurgeType *elem);
const SurgeType *ty_ref_of(const SurgeType *elem);
const SurgeType *ty_own_of(const SurgeType *elem);
const SurgeType *ty_channel_of(const SurgeType *elem);

// сравнение типов по структуре (shallow для известных видов)
bool ty_equal(const SurgeType *a, const SurgeType *b);

// печать имени типа
const char *ty_name(const SurgeType *t);

#endif // SURGE_TYPES_H