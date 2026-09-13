#include "rt_bignum_internal.h"

#include <float.h>
#include <inttypes.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

// The full runtime references both generated dispatch hooks. This stand starts
// no tasks: reaching either hook is a harness error, never a numeric answer.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
    fputs("bigfloat-from-f64-exact: unexpected poll dispatch\n", stderr);
    abort();
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    (void)out_dst;
    fputs("bigfloat-from-f64-exact: unexpected blocking dispatch\n", stderr);
    abort();
}

_Static_assert(FLT_RADIX == 2 && DBL_MANT_DIG == 53 && DBL_MIN_EXP == -1021 && DBL_MAX_EXP == 1024,
               "the exact input table requires binary64 doubles");
_Static_assert(SURGE_BIGNUM_MANTISSA_BITS == 256, "the table requires 256-bit mantissas");

typedef struct ExactRow {
    const char* name;
    double input;
    bool want_null;
    uint8_t neg;
    int32_t exp;
    uint32_t limb7;
    uint32_t limb6;
} ExactRow;

// These are literal binary values, not decimal round trips. A normalized
// BigFloat is (-1)^neg * mantissa * 2^exp with eight little-endian limbs.
// Aligning each hexadecimal significand's leading bit with bit 255 gives
// the two high limbs below; all six low limbs must be zero. No runtime
// constructor, decimal parser, frexp, or conversion back supplies the oracle.
// In particular, the neighbors of +/-2^63 differ by 1024 toward zero and
// 2048 away from zero, independently of the signed integer conversion API.
static const ExactRow rows[] = {
    {"positive_2p62", 0x1p62, false, 0, -193, 0x80000000, 0},
    {"negative_2p62", -0x1p62, false, 1, -193, 0x80000000, 0},
    {"positive_2p63", 0x1p63, false, 0, -192, 0x80000000, 0},
    {"negative_2p63", -0x1p63, false, 1, -192, 0x80000000, 0},
    {"positive_2p63_toward_zero", 0x1.fffffffffffffp62, false, 0, -193, 0xffffffff, 0xfffff800},
    {"negative_2p63_toward_zero", -0x1.fffffffffffffp62, false, 1, -193, 0xffffffff, 0xfffff800},
    {"positive_2p63_away_zero", 0x1.0000000000001p63, false, 0, -192, 0x80000000, 0x00000800},
    {"negative_2p63_away_zero", -0x1.0000000000001p63, false, 1, -192, 0x80000000, 0x00000800},
    {"positive_fraction", 0x1.56p5, false, 0, -250, 0xab000000, 0},
    {"negative_fraction", -0x1.56p5, false, 1, -250, 0xab000000, 0},
    {"binary_point_one", 0x1.999999999999ap-4, false, 0, -259, 0xcccccccc, 0xccccd000},
    {"next_above_one", 0x1.0000000000001p0, false, 0, -255, 0x80000000, 0x00000800},
    {"minimum_subnormal", 0x1p-1074, false, 0, -1329, 0x80000000, 0},
    {"minimum_normal", 0x1p-1022, false, 0, -1277, 0x80000000, 0},
    {"maximum_finite", 0x1.fffffffffffffp1023, false, 0, 768, 0xffffffff, 0xfffff800},
    {"positive_zero", 0.0, true, 0, 0, 0, 0},
    {"negative_zero", -0.0, true, 0, 0, 0, 0},
    {"nan", (double)NAN, true, 0, 0, 0, 0},
    {"positive_infinity", (double)INFINITY, true, 0, 0, 0, 0},
    {"negative_infinity", -(double)INFINITY, true, 0, 0, 0, 0},
};

static int run_row(const ExactRow* row) {
    SurgeBigFloat* value = rt_bigfloat_from_f64(row->input);
    bool is_null = value == NULL;
    uint32_t rc = value != NULL ? value->rc : 0;
    uint8_t neg = value != NULL ? value->neg : 0;
    int32_t exp = value != NULL ? value->exp : 0;
    uint32_t len = value != NULL && value->mant != NULL ? value->mant->len : 0;
    uint32_t actual[8] = {0};
    if (len == 8) {
        memcpy(actual, value->mant->limbs, sizeof(actual));
    }
    bool matched = row->want_null
                       ? is_null
                       : !is_null && rc == 1 && neg == row->neg && exp == row->exp && len == 8 &&
                             actual[7] == row->limb7 && actual[6] == row->limb6;
    for (size_t i = 0; i < 6; i++) {
        matched = matched && actual[i] == 0;
    }

    // A numerical mismatch must still free the returned owner. Memcheck can
    // therefore distinguish value failure from a leak in either path.
    rt_bigfloat_release(value);
    if (!matched) {
        printf("FAIL bigfloat-from-f64-exact: row=%s value mismatch "
               "null=%d rc=%" PRIu32 " neg=%u exp=%" PRId32 " len=%" PRIu32 " limbs=",
               row->name,
               (int)is_null,
               rc,
               (unsigned)neg,
               exp,
               len);
        for (size_t i = 8; i > 0; i--) {
            printf("%08" PRIx32, actual[i - 1]);
        }
        printf(" want_null=%d want_neg=%u want_exp=%" PRId32 " want_hi=%08" PRIx32 "%08" PRIx32
               " followed by six zero limbs\n",
               (int)row->want_null,
               (unsigned)row->neg,
               row->exp,
               row->limb7,
               row->limb6);
        return 1;
    }
    printf("bigfloat-from-f64-exact: row=%s exact=1 released=1\n", row->name);
    return 0;
}

int main(int argc, char** argv) {
    const char* name = argc == 2 ? argv[1] : getenv("SURGE_BIGFLOAT_EXACT_ROW");
    if (name != NULL) {
        for (size_t i = 0; i < sizeof(rows) / sizeof(rows[0]); i++) {
            if (strcmp(name, rows[i].name) == 0) {
                return run_row(&rows[i]);
            }
        }
    }
    fprintf(stderr, "bigfloat-from-f64-exact: unknown row \"%s\"\n", name != NULL ? name : "");
    return 2;
}
