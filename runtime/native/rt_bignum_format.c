#include "rt_bignum_internal.h"

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Formatting uses base-1e9 chunks for compact decimal conversion.
char* format_uint(const SurgeBigUint* u, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (u == NULL || u->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    const uint32_t base = SURGE_BIGNUM_DEC_BASE;
    SurgeBigUint* cur = bu_clone(u, err);
    if (cur == NULL) {
        if (err != NULL && *err == BN_OK) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    uint32_t* parts = NULL;
    size_t parts_len = 0;
    char* out = NULL;
    while (cur != NULL && cur->len != 0) {
        uint32_t rem = 0;
        bn_err tmp_err = BN_OK;
        SurgeBigUint* q = bu_div_mod_small(cur, base, &rem, &tmp_err);
        bu_free(cur);
        cur = NULL;
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(q);
            goto cleanup;
        }
        uint32_t* next = (uint32_t*)realloc(parts, (parts_len + 1) * sizeof(uint32_t));
        if (next == NULL) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            bu_free(q);
            goto cleanup;
        }
        parts = next;
        parts[parts_len++] = rem;
        cur = q;
    }
    bu_free(cur);
    if (parts_len == 0) {
        if (err != NULL && *err == BN_OK) {
            *err = BN_ERR_MAX_LIMBS;
        }
        goto cleanup;
    }
    char first_buf[32];
    int first_len = snprintf(first_buf, sizeof(first_buf), "%u", parts[parts_len - 1]);
    if (first_len < 0) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        goto cleanup;
    }
    size_t total_len = (size_t)first_len + (parts_len - 1) * 9;
    out = (char*)malloc(total_len + 1);
    if (out == NULL) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        goto cleanup;
    }
    memcpy(out, first_buf, (size_t)first_len);
    size_t offset = (size_t)first_len;
    for (size_t i = parts_len - 1; i-- > 0;) {
        char buf[16];
        int n = snprintf(buf, sizeof(buf), "%09u", parts[i]);
        if (n < 0) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            free(out);
            out = NULL;
            goto cleanup;
        }
        memcpy(out + offset, buf, (size_t)n);
        offset += (size_t)n;
        if (i == 0) {
            break;
        }
    }
    out[total_len] = 0;
cleanup:
    free(parts);
    return out;
}

char* format_int(const SurgeBigInt* i, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (i == NULL || i->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    SurgeBigUint* u = bu_clone(bi_as_uint(i), err);
    if (u == NULL) {
        if (err != NULL && *err == BN_OK) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    char* base = format_uint(u, err);
    if (base == NULL) {
        if (err != NULL && *err == BN_OK) {
            *err = BN_ERR_MAX_LIMBS;
        }
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
        size_t int_len = strlen(int_str);
        size_t frac_len = strlen(frac_str);
        size_t digits_len = int_len + frac_len;
        char* digits = (char*)malloc(digits_len + 1);
        if (digits == NULL) {
            return NULL;
        }
        memcpy(digits, int_str, int_len);
        memcpy(digits + int_len, frac_str, frac_len);
        digits[digits_len] = 0;
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
    snprintf(exp_buf, sizeof(exp_buf), "E-%d", -exp);
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
        bu_free(mant);
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    char* result = NULL;
    SurgeBigUint* int_mag = NULL;
    SurgeBigUint* int_part = NULL;
    SurgeBigUint* frac_part = NULL;
    SurgeBigUint* pow5 = NULL;
    SurgeBigUint* frac_digits = NULL;
    SurgeBigUint* tmp = NULL;
    char* int_str = NULL;
    char* frac_str = NULL;
    char* sci = NULL;
    if (f->exp >= 0) {
        bn_err tmp_err = BN_OK;
        int_mag = bu_shl(mant, (int)f->exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            goto cleanup;
        }
        bn_err fmt_err = BN_OK;
        int_str = format_uint(int_mag, &fmt_err);
        if (fmt_err != BN_OK) {
            if (err != NULL) {
                *err = fmt_err;
            }
            goto cleanup;
        }
        if (int_str == NULL) {
            goto cleanup;
        }
        if (!neg) {
            result = int_str;
            int_str = NULL;
            goto cleanup;
        }
        size_t len = strlen(int_str);
        result = (char*)malloc(len + 2);
        if (result == NULL) {
            goto cleanup;
        }
        result[0] = '-';
        memcpy(result + 1, int_str, len + 1);
        goto cleanup;
    }
    int64_t n64 = -(int64_t)f->exp;
    if (n64 < 0) {
        n64 = 0;
    }
    if (n64 > (int64_t)INT_MAX) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        goto cleanup;
    }
    int n = (int)n64;
    if (bu_is_zero(mant)) {
        result = (char*)malloc(2);
        if (result == NULL) {
            goto cleanup;
        }
        result[0] = '0';
        result[1] = 0;
        goto cleanup;
    }
    if (bu_bitlen(mant) >= (uint32_t)n && bu_is_odd(mant) == false) {
        int tz = 0;
        tmp = mant;
        while (tmp != NULL && tmp->len > 0 && (tmp->limbs[0] & 1u) == 0u) {
            tz++;
            bn_err tmp_err = BN_OK;
            SurgeBigUint* next = bu_shr(tmp, 1, &tmp_err);
            if (tmp != mant) {
                bu_free(tmp);
            }
            tmp = next;
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                if (tmp != NULL && tmp != mant) {
                    bu_free(tmp);
                    tmp = NULL;
                }
                goto cleanup;
            }
        }
        if (tmp != NULL && tmp != mant) {
            bu_free(tmp);
            tmp = NULL;
        }
        if (tz >= n) {
            bn_err tmp_err = BN_OK;
            int_mag = bu_shr(mant, n, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                goto cleanup;
            }
            bn_err fmt_err = BN_OK;
            int_str = format_uint(int_mag, &fmt_err);
            if (fmt_err != BN_OK) {
                if (err != NULL) {
                    *err = fmt_err;
                }
                goto cleanup;
            }
            if (int_str == NULL) {
                goto cleanup;
            }
            if (!neg) {
                result = int_str;
                int_str = NULL;
                goto cleanup;
            }
            size_t len = strlen(int_str);
            result = (char*)malloc(len + 2);
            if (result == NULL) {
                goto cleanup;
            }
            result[0] = '-';
            memcpy(result + 1, int_str, len + 1);
            goto cleanup;
        }
    }

    bn_err tmp_err = BN_OK;
    int_part = bu_shr(mant, n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    frac_part = bu_low_bits(mant, n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    pow5 = bu_pow5(n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    frac_digits = bu_mul(frac_part, pow5, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }

    bn_err fmt_err = BN_OK;
    int_str = format_uint(int_part, &fmt_err);
    if (fmt_err != BN_OK) {
        if (err != NULL) {
            *err = fmt_err;
        }
        goto cleanup;
    }
    fmt_err = BN_OK;
    frac_str = format_uint(frac_digits, &fmt_err);
    if (fmt_err != BN_OK) {
        if (err != NULL) {
            *err = fmt_err;
        }
        goto cleanup;
    }
    if (int_str == NULL || frac_str == NULL) {
        goto cleanup;
    }
    size_t frac_len = strlen(frac_str);
    if (frac_len < (size_t)n) {
        size_t pad = (size_t)n - frac_len;
        char* padded = (char*)malloc((size_t)n + 1);
        if (padded == NULL) {
            goto cleanup;
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
            result = int_str;
            int_str = NULL;
            goto cleanup;
        }
        size_t len = strlen(int_str);
        result = (char*)malloc(len + 2);
        if (result == NULL) {
            goto cleanup;
        }
        result[0] = '-';
        memcpy(result + 1, int_str, len + 1);
        goto cleanup;
    }
    sci = format_scientific(int_str, frac_str);
    if (sci == NULL) {
        goto cleanup;
    }
    if (!neg) {
        result = sci;
        sci = NULL;
        goto cleanup;
    }
    size_t len = strlen(sci);
    result = (char*)malloc(len + 2);
    if (result == NULL) {
        goto cleanup;
    }
    result[0] = '-';
    memcpy(result + 1, sci, len + 1);
    goto cleanup;
cleanup:
    if (tmp != NULL && tmp != mant) {
        bu_free(tmp);
    }
    bu_free(mant);
    bu_free(int_mag);
    bu_free(int_part);
    bu_free(frac_part);
    bu_free(pow5);
    bu_free(frac_digits);
    free(int_str);
    free(frac_str);
    free(sci);
    return result;
}
