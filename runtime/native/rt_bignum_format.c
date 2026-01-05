#include "rt_bignum_internal.h"

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Formatting uses base-1e9 chunks for compact decimal conversion.
char* format_uint(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    const uint32_t base = SURGE_BIGNUM_DEC_BASE;
    SurgeBigUint* cur = bu_clone(u, NULL);
    if (cur == NULL) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    uint32_t* parts = NULL;
    size_t parts_len = 0;
    while (cur != NULL && cur->len != 0) {
        uint32_t rem = 0;
        bn_err err = BN_OK;
        SurgeBigUint* q = bu_div_mod_small(cur, base, &rem, &err);
        bu_free(cur);
        cur = NULL;
        if (err != BN_OK) {
            bu_free(q);
            break;
        }
        uint32_t* next = (uint32_t*)realloc(parts, (parts_len + 1) * sizeof(uint32_t));
        if (next == NULL) {
            bu_free(q);
            break;
        }
        parts = next;
        parts[parts_len++] = rem;
        cur = q;
    }
    bu_free(cur);
    if (parts_len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    char first_buf[32];
    int first_len = snprintf(first_buf, sizeof(first_buf), "%u", parts[parts_len - 1]);
    if (first_len < 0) {
        return NULL;
    }
    size_t total_len = (size_t)first_len + (parts_len - 1) * 9;
    char* out = (char*)malloc(total_len + 1);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out, first_buf, (size_t)first_len);
    size_t offset = (size_t)first_len;
    for (size_t i = parts_len - 1; i-- > 0;) {
        char buf[16];
        int n = snprintf(buf, sizeof(buf), "%09u", parts[i]);
        if (n < 0) {
            break;
        }
        memcpy(out + offset, buf, (size_t)n);
        offset += (size_t)n;
        if (i == 0) {
            break;
        }
    }
    out[total_len] = 0;
    free(parts);
    return out;
}

char* format_int(const SurgeBigInt* i) {
    if (i == NULL || i->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    SurgeBigUint* u = bu_clone(bi_as_uint(i), NULL);
    char* base = format_uint(u);
    if (base == NULL) {
        bu_free(u);
        return NULL;
    }
    if (!i->neg) {
        bu_free(u);
        return base;
    }
    size_t len = strlen(base);
    char* out = (char*)malloc(len + 2);
    if (out == NULL) {
        free(base);
        bu_free(u);
        return NULL;
    }
    out[0] = '-';
    memcpy(out + 1, base, len + 1);
    free(base);
    bu_free(u);
    return out;
}

static char* format_scientific(const char* int_str, const char* frac_str) {
    if (int_str == NULL || frac_str == NULL) {
        return NULL;
    }
    if (strcmp(int_str, "0") != 0) {
        int exp = (int)strlen(int_str) - 1;
        size_t digits_len = strlen(int_str) + strlen(frac_str);
        char* digits = (char*)malloc(digits_len + 1);
        if (digits == NULL) {
            return NULL;
        }
        strcpy(digits, int_str);
        strcat(digits, frac_str);
        char first = digits[0];
        const char* rest = digits + 1;
        size_t rest_len = strlen(rest);
        char exp_buf[32];
        if (exp >= 0) {
            snprintf(exp_buf, sizeof(exp_buf), "E+%d", exp);
        } else {
            snprintf(exp_buf, sizeof(exp_buf), "E-%d", -exp);
        }
        size_t out_len = 1 + (rest_len ? 1 + rest_len : 0) + strlen(exp_buf);
        char* out = (char*)malloc(out_len + 1);
        if (out == NULL) {
            free(digits);
            return NULL;
        }
        size_t off = 0;
        out[off++] = first;
        if (rest_len > 0) {
            out[off++] = '.';
            memcpy(out + off, rest, rest_len);
            off += rest_len;
        }
        memcpy(out + off, exp_buf, strlen(exp_buf));
        out[out_len] = 0;
        free(digits);
        return out;
    }
    size_t i = 0;
    size_t frac_len = strlen(frac_str);
    while (i < frac_len && frac_str[i] == '0') {
        i++;
    }
    if (i >= frac_len) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    int exp = -(int)(i + 1);
    const char* digits = frac_str + i;
    char exp_buf[32];
    if (exp >= 0) {
        snprintf(exp_buf, sizeof(exp_buf), "E+%d", exp);
    } else {
        snprintf(exp_buf, sizeof(exp_buf), "E-%d", -exp);
    }
    char first = digits[0];
    const char* rest = digits + 1;
    size_t rest_len = strlen(rest);
    size_t out_len = 1 + (rest_len ? 1 + rest_len : 0) + strlen(exp_buf);
    char* out = (char*)malloc(out_len + 1);
    if (out == NULL) {
        return NULL;
    }
    size_t off = 0;
    out[off++] = first;
    if (rest_len > 0) {
        out[off++] = '.';
        memcpy(out + off, rest, rest_len);
        off += rest_len;
    }
    memcpy(out + off, exp_buf, strlen(exp_buf));
    out[out_len] = 0;
    return out;
}

// Format floats in a deterministic, VM-compatible scientific/decimal form.
char* format_float(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    bool neg = f->neg;
    SurgeBigUint* mant = bu_clone(f->mant, NULL);
    if (mant == NULL || mant->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (f->exp >= 0) {
        if ((int64_t)f->exp > (int64_t)INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        bn_err tmp_err = BN_OK;
        SurgeBigUint* int_mag = bu_shl(mant, (int)f->exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        char* s = format_uint(int_mag);
        if (s == NULL) {
            return NULL;
        }
        if (!neg) {
            return s;
        }
        size_t len = strlen(s);
        char* out = (char*)malloc(len + 2);
        if (out == NULL) {
            free(s);
            return NULL;
        }
        out[0] = '-';
        memcpy(out + 1, s, len + 1);
        free(s);
        return out;
    }
    int64_t n64 = -(int64_t)f->exp;
    if (n64 < 0) {
        n64 = 0;
    }
    if (n64 > (int64_t)INT_MAX) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    int n = (int)n64;
    if (bu_is_zero(mant)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    // Even when the value is < 1, we still need fractional digits (VM parity).
    if (bu_is_zero(mant)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (bu_bitlen(mant) >= (uint32_t)n && bu_is_odd(mant) == false) {
        int tz = 0;
        SurgeBigUint* tmp = mant;
        while (tmp != NULL && tmp->len > 0 && (tmp->limbs[0] & 1u) == 0u) {
            tz++;
            bn_err tmp_err = BN_OK;
            tmp = bu_shr(tmp, 1, &tmp_err);
            if (tmp_err != BN_OK) {
                break;
            }
        }
        if (tz >= n) {
            bn_err tmp_err = BN_OK;
            SurgeBigUint* int_mag = bu_shr(mant, n, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                return NULL;
            }
            char* s = format_uint(int_mag);
            if (s == NULL) {
                return NULL;
            }
            if (!neg) {
                return s;
            }
            size_t len = strlen(s);
            char* out = (char*)malloc(len + 2);
            if (out == NULL) {
                free(s);
                return NULL;
            }
            out[0] = '-';
            memcpy(out + 1, s, len + 1);
            free(s);
            return out;
        }
    }

    bn_err tmp_err = BN_OK;
    SurgeBigUint* int_part = bu_shr(mant, n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* frac_part = bu_low_bits(mant, n);
    SurgeBigUint* pow5 = bu_pow5(n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* frac_digits = bu_mul(frac_part, pow5, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }

    char* int_str = format_uint(int_part);
    char* frac_str = format_uint(frac_digits);
    if (int_str == NULL || frac_str == NULL) {
        return NULL;
    }
    size_t frac_len = strlen(frac_str);
    if (frac_len < (size_t)n) {
        size_t pad = (size_t)n - frac_len;
        char* padded = (char*)malloc((size_t)n + 1);
        if (padded == NULL) {
            return NULL;
        }
        memset(padded, '0', pad);
        memcpy(padded + pad, frac_str, frac_len + 1);
        free(frac_str);
        frac_str = padded;
    }
    while (strlen(frac_str) > 0 && frac_str[strlen(frac_str) - 1] == '0') {
        frac_str[strlen(frac_str) - 1] = 0;
    }
    if (strlen(frac_str) == 0) {
        if (!neg) {
            return int_str;
        }
        size_t len = strlen(int_str);
        char* out = (char*)malloc(len + 2);
        if (out == NULL) {
            return NULL;
        }
        out[0] = '-';
        memcpy(out + 1, int_str, len + 1);
        free(int_str);
        return out;
    }
    char* sci = format_scientific(int_str, frac_str);
    if (sci == NULL) {
        return NULL;
    }
    if (!neg) {
        return sci;
    }
    size_t len = strlen(sci);
    char* out = (char*)malloc(len + 2);
    if (out == NULL) {
        free(sci);
        return NULL;
    }
    out[0] = '-';
    memcpy(out + 1, sci, len + 1);
    free(sci);
    return out;
}
