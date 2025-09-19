#include "sbc.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define STATIC_ASSERT(COND, MSG) typedef char static_assertion_##MSG[(COND) ? 1 : -1]

STATIC_ASSERT(sizeof(SbcHeader) == 36, sbc_header_size_expected);
STATIC_ASSERT(sizeof(SbcFuncDesc) == 20, sbc_funcdesc_size_expected);

static uint32_t read_u32_le(const uint8_t *p) {
    return (uint32_t)p[0] |
           ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) |
           ((uint32_t)p[3] << 24);
}

static bool validate_ranges(const SbcHeader *hdr, size_t blob_len) {
    const uint32_t off_const = hdr->off_const;
    const uint32_t off_funcs = hdr->off_funcs;
    const uint32_t off_code = hdr->off_code;
    const uint32_t sz_const = hdr->sz_const;
    const uint32_t sz_funcs = hdr->sz_funcs;
    const uint32_t sz_code = hdr->sz_code;

    if (off_const > blob_len || off_funcs > blob_len || off_code > blob_len) {
        return false;
    }
    if ((uint64_t)off_const + sz_const > blob_len) return false;
    if ((uint64_t)off_funcs + sz_funcs > blob_len) return false;
    if ((uint64_t)off_code + sz_code > blob_len) return false;

    return true;
}

bool sbc_load_from_file(const char *path, SbcImage *out_img) {
    if (!path || !out_img) {
        return false;
    }
    memset(out_img, 0, sizeof(*out_img));

    FILE *f = fopen(path, "rb");
    if (!f) {
        return false;
    }

    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        return false;
    }
    long file_len = ftell(f);
    if (file_len < 0) {
        fclose(f);
        return false;
    }
    if (fseek(f, 0, SEEK_SET) != 0) {
        fclose(f);
        return false;
    }

    uint8_t *blob = (uint8_t*)malloc((size_t)file_len);
    if (!blob) {
        fclose(f);
        return false;
    }

    size_t read_len = fread(blob, 1, (size_t)file_len, f);
    fclose(f);
    if (read_len != (size_t)file_len) {
        free(blob);
        return false;
    }

    if ((size_t)file_len < sizeof(SbcHeader)) {
        free(blob);
        return false;
    }

    SbcHeader hdr;
    memcpy(&hdr, blob, sizeof(SbcHeader));

    if (hdr.magic != SBC_MAGIC) {
        free(blob);
        return false;
    }
    if (hdr.ver_major != SBC_VERSION_MAJOR || hdr.ver_minor != SBC_VERSION_MINOR) {
        free(blob);
        return false;
    }

    if (!validate_ranges(&hdr, (size_t)file_len)) {
        free(blob);
        return false;
    }

    if ((hdr.sz_const % 4u) != 0u) {
        free(blob);
        return false;
    }
    if ((hdr.sz_funcs % sizeof(SbcFuncDesc)) != 0u) {
        free(blob);
        return false;
    }

    const uint8_t *const_sec = blob + hdr.off_const;
    if (hdr.sz_const < 4u) {
        free(blob);
        return false;
    }
    uint32_t const_count = read_u32_le(const_sec);
    const uint8_t *const_data = const_sec + 4u;
    const uint8_t *const_end = const_sec + hdr.sz_const;

    const uint8_t *p = const_data;
    for (uint32_t idx = 0; idx < const_count; ++idx) {
        if (p >= const_end) {
            free(blob);
            return false;
        }
        uint8_t kind = *p++;
        switch (kind) {
            case SBC_CONST_I64:
            case SBC_CONST_F64:
                if ((size_t)(const_end - p) < sizeof(uint64_t)) {
                    free(blob);
                    return false;
                }
                p += sizeof(uint64_t);
                break;
            case SBC_CONST_STR: {
                if ((size_t)(const_end - p) < sizeof(uint32_t)) {
                    free(blob);
                    return false;
                }
                uint32_t len = read_u32_le(p);
                p += sizeof(uint32_t);
                if ((uint64_t)(const_end - p) < len) {
                    free(blob);
                    return false;
                }
                p += len;
                uint32_t aligned = sbc_align4((uint32_t)(p - const_sec));
                p = const_sec + aligned;
                break;
            }
            default:
                free(blob);
                return false;
        }
    }
    if (p != const_end) {
        /* allow padding zeroes after constants */
        const uint8_t *pad = p;
        while (pad < const_end) {
            if (*pad != 0) {
                free(blob);
                return false;
            }
            ++pad;
        }
    }

    const uint8_t *func_sec = blob + hdr.off_funcs;
    uint32_t func_count = hdr.sz_funcs / sizeof(SbcFuncDesc);
    SbcFuncDesc *funcs = NULL;
    if (func_count > 0) {
        funcs = (SbcFuncDesc*)malloc(hdr.sz_funcs);
        if (!funcs) {
            free(blob);
            return false;
        }
        memcpy(funcs, func_sec, hdr.sz_funcs);
        for (uint32_t i=0; i<func_count; ++i) {
            if (funcs[i].name_idx >= const_count) { free(funcs); free(blob); return false; }
            if ((uint64_t)funcs[i].code_off + funcs[i].code_len > hdr.sz_code) { free(funcs); free(blob); return false; }
        }
    }

    const uint8_t *code_sec = blob + hdr.off_code;

    out_img->hdr = hdr;
    out_img->blob = blob;
    out_img->blob_len = (size_t)file_len;
    out_img->const_sec = const_sec;
    out_img->const_data = const_data;
    out_img->const_count = const_count;
    out_img->funcs = funcs;
    out_img->func_count = func_count;
    out_img->global_count = hdr.global_count;
    out_img->code_sec = code_sec;

    return true;
}

void sbc_unload(SbcImage *img) {
    if (!img) {
        return;
    }
    free(img->funcs);
    free(img->blob);
    memset(img, 0, sizeof(*img));
}

bool sbc_const_at(const SbcImage *img, uint32_t index, SbcConstKind *out_kind,
                  const void **out_ptr, uint32_t *out_len) {
    if (!img || index >= img->const_count) {
        return false;
    }

    const uint8_t *p = img->const_data;
    const uint8_t *const_end = img->const_sec + img->hdr.sz_const;
    for (uint32_t idx = 0; idx < img->const_count; ++idx) {
        if (p >= const_end) {
            return false;
        }
        uint8_t kind = *p++;
        if (idx == index) {
            if (out_kind) {
                *out_kind = (SbcConstKind)kind;
            }
            switch (kind) {
                case SBC_CONST_I64:
                    if ((size_t)(const_end - p) < sizeof(int64_t)) {
                        return false;
                    }
                    if (out_ptr) {
                        *out_ptr = p;
                    }
                    if (out_len) {
                        *out_len = sizeof(int64_t);
                    }
                    return true;
                case SBC_CONST_F64:
                    if ((size_t)(const_end - p) < sizeof(double)) {
                        return false;
                    }
                    if (out_ptr) {
                        *out_ptr = p;
                    }
                    if (out_len) {
                        *out_len = sizeof(double);
                    }
                    return true;
                case SBC_CONST_STR: {
                    if ((size_t)(const_end - p) < sizeof(uint32_t)) {
                        return false;
                    }
                    uint32_t len = read_u32_le(p);
                    const uint8_t *str_bytes = p + sizeof(uint32_t);
                    if ((uint64_t)(const_end - str_bytes) < len) {
                        return false;
                    }
                    if (out_ptr) {
                        *out_ptr = str_bytes;
                    }
                    if (out_len) {
                        *out_len = len;
                    }
                    return true;
                }
                default:
                    return false;
            }
        }

        switch (kind) {
            case SBC_CONST_I64:
            case SBC_CONST_F64:
                p += sizeof(uint64_t);
                break;
            case SBC_CONST_STR: {
                uint32_t len = read_u32_le(p);
                p += sizeof(uint32_t) + len;
                uint32_t aligned = sbc_align4((uint32_t)(p - img->const_sec));
                p = img->const_sec + aligned;
                break;
            }
            default:
                return false;
        }
    }

    return false;
}
