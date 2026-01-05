#include "rt_bignum_internal.h"

#include <errno.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Runtime entry points called from LLVM lowering and intrinsics.
static bool string_span(void* s, const uint8_t** out_ptr, uint64_t* out_len) {
    if (out_ptr != NULL) {
        *out_ptr = NULL;
    }
    if (out_len != NULL) {
        *out_len = 0;
    }
    if (s == NULL) {
        return false;
    }
    const uint8_t* ptr = rt_string_ptr(s);
    uint64_t len = rt_string_len_bytes(s);
    if (out_ptr != NULL) {
        *out_ptr = ptr;
    }
    if (out_len != NULL) {
        *out_len = len;
    }
    return true;
}

void* rt_bigint_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, false, true, &mag);
    if (err != BN_OK) {
        bignum_panic_err(err);
        bu_free(mag);
        return NULL;
    }
    if (mag == NULL || mag->len == 0) {
        bu_free(mag);
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* out = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK || out == NULL) {
        bignum_panic_err(tmp_err);
        bu_free(mag);
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    bu_free(mag);
    return (void*)out;
}

void* rt_biguint_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, false, true, &mag);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    return (void*)mag;
}

void* rt_bigfloat_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigFloat* out = NULL;
    bn_err err = parse_float_string(ptr, (size_t)len, &out);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    return (void*)out;
}

bool rt_parse_bigint(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigInt* res = NULL;
    bn_err err = parse_int_string(ptr, (size_t)len, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

bool rt_parse_biguint(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigUint* res = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, true, false, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

bool rt_parse_bigfloat(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigFloat* res = NULL;
    bn_err err = parse_float_string(ptr, (size_t)len, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

void* rt_string_from_bigint(void* v) {
    char* s = format_int((const SurgeBigInt*)v);
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_string_from_biguint(void* v) {
    char* s = format_uint((const SurgeBigUint*)v);
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_string_from_bigfloat(void* v) {
    bn_err err = BN_OK;
    char* s = format_float((const SurgeBigFloat*)v, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return rt_string_from_bytes(NULL, 0);
    }
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_bigint_from_i64(int64_t value) {
    return (void*)bi_from_i64(value);
}

void* rt_bigint_from_u64(uint64_t value) {
    return (void*)bi_from_u64(value);
}

void* rt_biguint_from_u64(uint64_t value) {
    return (void*)bu_from_u64(value);
}

void* rt_bigfloat_from_i64(int64_t value) {
    bn_err err = BN_OK;
    SurgeBigInt* i = bi_from_i64(value);
    SurgeBigFloat* f = bf_from_int(i, &err);
    bi_free(i);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)f;
}

void* rt_bigfloat_from_u64(uint64_t value) {
    bn_err err = BN_OK;
    SurgeBigUint* u = bu_from_u64(value);
    SurgeBigFloat* f = bf_from_uint(u, &err);
    bu_free(u);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)f;
}

void* rt_bigfloat_from_f64(double value) {
    if (isnan(value) || isinf(value)) {
        return NULL;
    }
    char buf[64];
    int n = snprintf(buf, sizeof(buf), "%.17g", value);
    if (n < 0) {
        return NULL;
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    return rt_bigfloat_from_literal((const uint8_t*)buf, (uint64_t)n);
}

bool rt_bigint_to_i64(void* v, int64_t* out) {
    return bi_to_i64((const SurgeBigInt*)v, out);
}

bool rt_biguint_to_u64(void* v, uint64_t* out) {
    return bu_to_u64((const SurgeBigUint*)v, out);
}

bool rt_bigfloat_to_f64(void* v, double* out) {
    if (out != NULL) {
        *out = 0.0;
    }
    if (v == NULL || bf_is_zero((const SurgeBigFloat*)v)) {
        return true;
    }
    bn_err err = BN_OK;
    char* s = format_float((const SurgeBigFloat*)v, &err);
    if (err != BN_OK || s == NULL) {
        return false;
    }
    errno = 0;
    char* endptr = NULL;
    double val = strtod(s, &endptr);
    bool ok = !(errno != 0 || endptr == s || *endptr != 0);
    free(s);
    if (ok && out != NULL) {
        *out = val;
    }
    return ok;
}

void* rt_bigint_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_add((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_sub((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_mul((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_div_mod((const SurgeBigInt*)a, (const SurgeBigInt*)b, NULL, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* rem = NULL;
    bi_div_mod((const SurgeBigInt*)a, (const SurgeBigInt*)b, &rem, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)rem;
}

void* rt_bigint_neg(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_neg((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_abs(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_abs_val((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

int32_t rt_bigint_cmp(void* a, void* b) {
    return (int32_t)bi_cmp((const SurgeBigInt*)a, (const SurgeBigInt*)b);
}

void* rt_bigint_bit_and(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_and, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_bit_or(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_or, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_bit_xor(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_xor, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_shl(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shl((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return (void*)out;
}

void* rt_bigint_shr(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shr((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return (void*)out;
}

void* rt_biguint_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_add((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_sub((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_mul((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_div_mod((const SurgeBigUint*)a, (const SurgeBigUint*)b, NULL, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* rem = NULL;
    bu_div_mod((const SurgeBigUint*)a, (const SurgeBigUint*)b, &rem, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)rem;
}

int32_t rt_biguint_cmp(void* a, void* b) {
    return (int32_t)bu_cmp((const SurgeBigUint*)a, (const SurgeBigUint*)b);
}

void* rt_biguint_bit_and(void* a, void* b) {
    SurgeBigUint* out = bu_and((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_bit_or(void* a, void* b) {
    SurgeBigUint* out = bu_or((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_bit_xor(void* a, void* b) {
    SurgeBigUint* out = bu_xor((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_shl(void* a, void* b) {
    int shift = 0;
    if (!shift_count_from_biguint((const SurgeBigUint*)b, &shift)) {
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shl((const SurgeBigUint*)a, shift, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_shr(void* a, void* b) {
    int shift = 0;
    if (!shift_count_from_biguint((const SurgeBigUint*)b, &shift)) {
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shr((const SurgeBigUint*)a, shift, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_add((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_sub((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mul((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_div((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mod((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_neg(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_neg((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_abs(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_abs((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

int32_t rt_bigfloat_cmp(void* a, void* b) {
    return (int32_t)bf_cmp((const SurgeBigFloat*)a, (const SurgeBigFloat*)b);
}

void* rt_bigint_to_biguint(void* a) {
    const SurgeBigInt* src = (const SurgeBigInt*)a;
    if (src != NULL && src->neg && !bi_is_zero(src)) {
        bignum_panic("cannot convert negative int to uint");
        return NULL;
    }
    return (void*)bu_clone(bi_as_uint(src), NULL);
}

void* rt_biguint_to_bigint(void* a) {
    const SurgeBigUint* src = (const SurgeBigUint*)a;
    if (src == NULL || src->len == 0) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_alloc(src->len, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, src->limbs, (size_t)src->len * sizeof(uint32_t));
    out->len = src->len;
    return (void*)out;
}

void* rt_bigint_to_bigfloat(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_int((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_to_bigfloat(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_uint((const SurgeBigUint*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_to_bigint(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bf_to_int_trunc((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_to_biguint(void* a) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bf_to_uint_trunc((const SurgeBigFloat*)a, &err);
    if (err == BN_ERR_UNDERFLOW) {
        bignum_panic("cannot convert negative float to uint");
        return NULL;
    }
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}
