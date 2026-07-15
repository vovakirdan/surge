#include "rt_bignum_internal.h"

#include <errno.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Runtime entry points called from LLVM lowering and intrinsics.
//
// int/uint values arrive here as tagged words (see rt_bignum_tag.h): a low-bit
// tag marks an inline fixnum, an even non-NULL word is an aligned heap bignum,
// and NULL is zero. Each entry decodes its operands, takes an allocation-free
// fast path when every operand is inline-or-zero, and otherwise promotes to the
// unchanged bi_*/bu_* heap implementation and demotes the result back inline
// when it fits. Genuinely large results (outside the inline range) stay on the
// heap. Bigfloat is untagged and its functions are unaffected.
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

// ---- fixnum <-> heap bridging -------------------------------------------

// Signed int64 result -> tagged word: inline when it fits, heap otherwise.
void* bi_from_i64_tagged(int64_t v) {
    bool ok = false;
    void* w = fixi_box(v, &ok);
    if (ok) {
        return w;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_from_i64(v, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

// Unsigned magnitude -> tagged signed-int word (used by uint->int conversions
// and unsigned literals that target int).
void* bi_from_u64_tagged(uint64_t v) {
    if (v <= (uint64_t)SURGE_FIXI_MAX) {
        bool ok = false;
        void* w = fixi_box((int64_t)v, &ok);
        if (ok) {
            return w;
        }
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_from_u64(v, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

// Unsigned uint64 result -> tagged word: inline when it fits, heap otherwise.
void* bu_from_u64_tagged(uint64_t v) {
    bool ok = false;
    void* w = fixu_box(v, &ok);
    if (ok) {
        return w;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_from_u64(v, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

bi_operand bi_promote(const void* w) {
    bi_operand o = {NULL, NULL};
    if (w == NULL) {
        return o;
    }
    if (fix_is_inline(w)) {
        o.owned = bi_from_i64(fixi_value(w), NULL);
        o.p = o.owned;
    } else {
        o.p = (const SurgeBigInt*)w;
    }
    return o;
}

void bi_operand_release(bi_operand* o) {
    if (o->owned != NULL) {
        bi_free(o->owned);
        o->owned = NULL;
    }
}

// Take ownership of a fresh heap result and shrink it to an inline fixnum when
// it fits, freeing the heap block. Large results stay on the heap.
void* bi_finish(SurgeBigInt* r) {
    if (r == NULL) {
        return NULL;
    }
    int64_t v = 0;
    if (bi_to_i64(r, &v)) {
        bool ok = false;
        void* w = fixi_box(v, &ok);
        if (ok) {
            bi_free(r);
            return w;
        }
    }
    return (void*)r;
}

bu_operand bu_promote(const void* w) {
    bu_operand o = {NULL, NULL};
    if (w == NULL) {
        return o;
    }
    if (fix_is_inline(w)) {
        o.owned = bu_from_u64(fixu_value(w), NULL);
        o.p = o.owned;
    } else {
        o.p = (const SurgeBigUint*)w;
    }
    return o;
}

void bu_operand_release(bu_operand* o) {
    if (o->owned != NULL) {
        bu_free(o->owned);
        o->owned = NULL;
    }
}

void* bu_finish(SurgeBigUint* r) {
    if (r == NULL) {
        return NULL;
    }
    uint64_t v = 0;
    if (bu_to_u64(r, &v)) {
        bool ok = false;
        void* w = fixu_box(v, &ok);
        if (ok) {
            bu_free(r);
            return w;
        }
    }
    return (void*)r;
}

// ---- literals -----------------------------------------------------------

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
    return bi_finish(out);
}

void* rt_biguint_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, false, true, &mag);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    return bu_finish(mag);
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
        *out = bi_finish(res);
    } else {
        bi_free(res);
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
        *out = bu_finish(res);
    } else {
        bu_free(res);
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

// ---- formatting ---------------------------------------------------------

void* rt_string_from_bigint(void* v) {
    int64_t iv = 0;
    if (fixi_as_i64(v, &iv)) {
        char buf[24];
        int n = snprintf(buf, sizeof(buf), "%lld", (long long)iv);
        if (n < 0) {
            n = 0;
        }
        return rt_string_from_bytes((const uint8_t*)buf, (uint64_t)n);
    }
    bn_err err = BN_OK;
    char* s = format_int((const SurgeBigInt*)v, &err);
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

void* rt_string_from_biguint(void* v) {
    uint64_t uv = 0;
    if (fixu_as_u64(v, &uv)) {
        char buf[24];
        int n = snprintf(buf, sizeof(buf), "%llu", (unsigned long long)uv);
        if (n < 0) {
            n = 0;
        }
        return rt_string_from_bytes((const uint8_t*)buf, (uint64_t)n);
    }
    bn_err err = BN_OK;
    char* s = format_uint((const SurgeBigUint*)v, &err);
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

// ---- constructors from native words -------------------------------------

void* rt_bigint_from_i64(int64_t value) {
    return bi_from_i64_tagged(value);
}

void* rt_bigint_from_u64(uint64_t value) {
    return bi_from_u64_tagged(value);
}

void* rt_biguint_from_u64(uint64_t value) {
    return bu_from_u64_tagged(value);
}

void* rt_bigfloat_from_i64(int64_t value) {
    bn_err err = BN_OK;
    SurgeBigInt* i = bi_from_i64(value, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    SurgeBigFloat* f = bf_from_int(i, &err);
    bi_free(i);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)f;
}

void* rt_bigfloat_from_u64(uint64_t value) {
    bn_err err = BN_OK;
    SurgeBigUint* u = bu_from_u64(value, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
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
    int64_t iv = 0;
    if (fixi_as_i64(v, &iv)) {
        if (out != NULL) {
            *out = iv;
        }
        return true;
    }
    return bi_to_i64((const SurgeBigInt*)v, out);
}

bool rt_biguint_to_u64(void* v, uint64_t* out) {
    uint64_t uv = 0;
    if (fixu_as_u64(v, &uv)) {
        if (out != NULL) {
            *out = uv;
        }
        return true;
    }
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

// ---- bigfloat arithmetic (untagged) -------------------------------------

void* rt_bigfloat_add(const void* a, const void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_add((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_sub(const void* a, const void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_sub((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mul(const void* a, const void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mul((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_div(const void* a, const void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_div((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mod(const void* a, const void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mod((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_neg(const void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_neg((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_abs(const void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_abs((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

int32_t rt_bigfloat_cmp(const void* a, const void* b) {
    return (int32_t)bf_cmp((const SurgeBigFloat*)a, (const SurgeBigFloat*)b);
}

void* rt_bigfloat_clone(const void* a) {
    if (a == NULL) {
        return NULL; // NULL is the zero float; nothing to allocate.
    }
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_clone((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void rt_bigfloat_free(void* a) {
    if (a == NULL) {
        return;
    }
    bf_free((SurgeBigFloat*)a);
}

// ---- conversions --------------------------------------------------------

void* rt_bigint_to_biguint(const void* a) {
    int64_t av = 0;
    if (fixi_as_i64(a, &av)) {
        if (av < 0) {
            bignum_panic("cannot convert negative int to uint");
            return NULL;
        }
        return bu_from_u64_tagged((uint64_t)av);
    }
    const SurgeBigInt* src = (const SurgeBigInt*)a;
    if (src != NULL && src->neg && !bi_is_zero(src)) {
        bignum_panic("cannot convert negative int to uint");
        return NULL;
    }
    return bu_finish(bu_clone(bi_as_uint(src), NULL));
}

void* rt_biguint_to_bigint(const void* a) {
    uint64_t uv = 0;
    if (fixu_as_u64(a, &uv)) {
        return bi_from_u64_tagged(uv);
    }
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
    return bi_finish(out);
}

void* rt_bigint_to_bigfloat(const void* a) {
    bi_operand ao = bi_promote(a);
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_int(ao.p, &err);
    bi_operand_release(&ao);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_to_bigfloat(const void* a) {
    bu_operand ao = bu_promote(a);
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_uint(ao.p, &err);
    bu_operand_release(&ao);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_to_bigint(const void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bf_to_int_trunc((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigfloat_to_biguint(const void* a) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bf_to_uint_trunc((const SurgeBigFloat*)a, &err);
    if (err == BN_ERR_UNDERFLOW) {
        bignum_panic("cannot convert negative float to uint");
        return NULL;
    }
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}
