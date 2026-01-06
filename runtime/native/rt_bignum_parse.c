#include "rt_bignum_internal.h"

#include <ctype.h>
#include <stdlib.h>
#include <string.h>

// Parsing accepts optional whitespace, sign, and numeric base prefixes.
static int digit_value(char ch, uint32_t base, bool* ok) {
    if (ok != NULL) {
        *ok = true;
    }
    if (ch >= '0' && ch <= '9') {
        int d = ch - '0';
        if ((uint32_t)d >= base) {
            if (ok != NULL) {
                *ok = false;
            }
        }
        return d;
    }
    if (base == 16 && ch >= 'a' && ch <= 'f') {
        return 10 + (ch - 'a');
    }
    if (base == 16 && ch >= 'A' && ch <= 'F') {
        return 10 + (ch - 'A');
    }
    if (ok != NULL) {
        *ok = false;
    }
    return 0;
}

bn_err parse_uint_string(
    const uint8_t* data, size_t len, bool allow_plus, bool allow_prefix, SurgeBigUint** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    if (allow_plus && data[start] == '+') {
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    uint32_t base = 10;
    if (allow_prefix && end - start > 2 && data[start] == '0') {
        char c = (char)data[start + 1];
        if (c == 'x' || c == 'X') {
            base = 16;
            start += 2;
        } else if (c == 'b' || c == 'B') {
            base = 2;
            start += 2;
        } else if (c == 'o' || c == 'O') {
            base = 8;
            start += 2;
        }
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    SurgeBigUint* cur = NULL;
    for (size_t i = start; i < end; i++) {
        uint8_t ch = data[i];
        if (ch == '_') {
            continue;
        }
        bool ok = false;
        int d = digit_value((char)ch, base, &ok);
        if (!ok) {
            return BN_ERR_NEG_SHIFT;
        }
        bn_err tmp_err = BN_OK;
        SurgeBigUint* prev = cur;
        cur = bu_mul_small(prev, base, &tmp_err);
        if (tmp_err != BN_OK) {
            bu_free(prev);
            bu_free(cur);
            return tmp_err;
        }
        prev = cur;
        cur = bu_add_small(prev, (uint32_t)d, &tmp_err);
        if (tmp_err != BN_OK) {
            bu_free(prev);
            bu_free(cur);
            return tmp_err;
        }
    }
    if (out != NULL) {
        *out = cur;
    }
    return BN_OK;
}

bn_err parse_int_string(const uint8_t* data, size_t len, SurgeBigInt** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    bool neg = false;
    if (data[start] == '+') {
        start++;
    } else if (data[start] == '-') {
        neg = true;
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(data + start, end - start, false, false, &mag);
    if (err != BN_OK) {
        return err;
    }
    if (mag == NULL || mag->len == 0) {
        bu_free(mag);
        return BN_OK;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* res = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK) {
        bu_free(mag);
        return tmp_err;
    }
    res->neg = neg ? 1 : 0;
    memcpy(res->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    res->len = mag->len;
    if (out != NULL) {
        *out = res;
    }
    bu_free(mag);
    return BN_OK;
}

bn_err parse_float_string(const uint8_t* data, size_t len, SurgeBigFloat** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    bool neg = false;
    if (data[start] == '+') {
        start++;
    } else if (data[start] == '-') {
        neg = true;
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t i = start;
    uint8_t* digits = (uint8_t*)malloc(len + 1);
    if (digits == NULL) {
        return BN_ERR_MAX_LIMBS;
    }
    size_t digits_len = 0;
    while (i < end && data[i] >= '0' && data[i] <= '9') {
        digits[digits_len++] = data[i];
        i++;
    }
    if (digits_len == 0) {
        free(digits);
        return BN_ERR_NEG_SHIFT;
    }
    int frac_digits = 0;
    if (i < end && data[i] == '.') {
        i++;
        while (i < end && data[i] >= '0' && data[i] <= '9') {
            digits[digits_len++] = data[i];
            frac_digits++;
            i++;
        }
    }
    int exp10 = 0;
    if (i < end && (data[i] == 'e' || data[i] == 'E')) {
        i++;
        if (i >= end) {
            free(digits);
            return BN_ERR_NEG_SHIFT;
        }
        bool exp_neg = false;
        if (data[i] == '+') {
            i++;
        } else if (data[i] == '-') {
            exp_neg = true;
            i++;
        }
        if (i >= end || data[i] < '0' || data[i] > '9') {
            free(digits);
            return BN_ERR_NEG_SHIFT;
        }
        int val = 0;
        const int max_exp = SURGE_BIGNUM_MAX_EXP10;
        while (i < end && data[i] >= '0' && data[i] <= '9') {
            if (val > max_exp) {
                free(digits);
                return BN_ERR_NEG_SHIFT;
            }
            val = val * 10 + (int)(data[i] - '0');
            i++;
        }
        exp10 = exp_neg ? -val : val;
    }
    if (i != end) {
        free(digits);
        return BN_ERR_NEG_SHIFT;
    }
    size_t leading = 0;
    while (leading < digits_len && digits[leading] == '0') {
        leading++;
    }
    if (leading == digits_len) {
        free(digits);
        return BN_OK;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* n = NULL;
    tmp_err = parse_uint_string(digits + leading, digits_len - leading, false, false, &n);
    free(digits);
    if (tmp_err != BN_OK) {
        bu_free(n);
        return tmp_err;
    }
    int k = exp10 - frac_digits;
    SurgeBigUint* num = n;
    SurgeBigUint* den = bu_from_u64(1, &tmp_err);
    if (tmp_err != BN_OK || den == NULL) {
        bu_free(num);
        return (tmp_err != BN_OK) ? tmp_err : BN_ERR_MAX_LIMBS;
    }
    if (k >= 0) {
        SurgeBigUint* pow = bu_pow10(k, &tmp_err);
        if (pow == NULL && tmp_err == BN_OK) {
            tmp_err = BN_ERR_MAX_LIMBS;
        }
        if (tmp_err != BN_OK || pow == NULL) {
            bu_free(num);
            bu_free(den);
            return tmp_err;
        }
        SurgeBigUint* next_num = bu_mul(num, pow, &tmp_err);
        bu_free(pow);
        if (next_num == NULL && tmp_err == BN_OK) {
            tmp_err = BN_ERR_MAX_LIMBS;
        }
        if (tmp_err != BN_OK || next_num == NULL) {
            bu_free(num);
            bu_free(den);
            return tmp_err;
        }
        bu_free(num);
        num = next_num;
    } else {
        SurgeBigUint* pow = bu_pow10(-k, &tmp_err);
        if (pow == NULL && tmp_err == BN_OK) {
            tmp_err = BN_ERR_MAX_LIMBS;
        }
        if (tmp_err != BN_OK || pow == NULL) {
            bu_free(num);
            bu_free(den);
            return tmp_err;
        }
        bu_free(den);
        den = pow;
    }
    SurgeBigFloat* f = bf_from_ratio(neg, num, den, &tmp_err);
    if (tmp_err != BN_OK) {
        bu_free(num);
        bu_free(den);
        return tmp_err;
    }
    if (out != NULL) {
        *out = f;
    }
    bu_free(num);
    bu_free(den);
    return BN_OK;
}
