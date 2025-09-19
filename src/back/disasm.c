#include "disasm.h"

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "opcodes.h"
#include "sbc.h"

static uint16_t read_u16_le(const uint8_t *p) {
    return (uint16_t)(p[0] | (uint16_t)(p[1] << 8));
}

static uint32_t read_u32_le(const uint8_t *p) {
    return (uint32_t)p[0] |
           ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) |
           ((uint32_t)p[3] << 24);
}

static int32_t read_i32_le(const uint8_t *p) {
    return (int32_t)read_u32_le(p);
}

static uint64_t read_u64_le(const uint8_t *p) {
    return (uint64_t)p[0] |
           ((uint64_t)p[1] << 8) |
           ((uint64_t)p[2] << 16) |
           ((uint64_t)p[3] << 24) |
           ((uint64_t)p[4] << 32) |
           ((uint64_t)p[5] << 40) |
           ((uint64_t)p[6] << 48) |
           ((uint64_t)p[7] << 56);
}

static double read_f64_le(const uint8_t *p) {
    double val;
    uint64_t bits = read_u64_le(p);
    memcpy(&val, &bits, sizeof(val));
    return val;
}

static void print_string_escaped(FILE *out, const char *data, uint32_t len) {
    fputc('"', out);
    for (uint32_t i = 0; i < len; ++i) {
        unsigned char ch = (unsigned char)data[i];
        switch (ch) {
            case '\\': fputs("\\\\", out); break;
            case '"': fputs("\\\"", out); break;
            case '\n': fputs("\\n", out); break;
            case '\r': fputs("\\r", out); break;
            case '	': fputs("\\t", out); break;
            default:
                if (ch < 0x20 || ch > 0x7E) {
                    fprintf(out, "\\x%02X", ch);
                } else {
                    fputc(ch, out);
                }
        }
    }
    fputc('"', out);
}

static void print_const_ref(const SbcImage *img, uint32_t index, FILE *out) {
    if (!img) {
        fprintf(out, "#%u", index);
        return;
    }
    SbcConstKind kind;
    const void *payload;
    uint32_t len;
    if (!sbc_const_at(img, index, &kind, &payload, &len)) {
        fprintf(out, "#%u", index);
        return;
    }
    const uint8_t *bytes = (const uint8_t *)payload;
    switch (kind) {
        case SBC_CONST_I64: {
            int64_t v = (int64_t)read_u64_le(bytes);
            fprintf(out, "#%u:i64=%" PRId64, index, v);
            break;
        }
        case SBC_CONST_F64: {
            double d = read_f64_le(bytes);
            fprintf(out, "#%u:f64=%g", index, d);
            break;
        }
        case SBC_CONST_STR:
            fprintf(out, "#%u:str=", index);
            print_string_escaped(out, (const char*)payload, len);
            break;
        default:
            fprintf(out, "#%u:kind=%u", index, (unsigned)kind);
            break;
    }
}

static const char *resolve_func_name(const SbcImage *img, uint32_t func_index) {
    if (!img || func_index >= img->func_count) {
        return NULL;
    }
    const SbcFuncDesc *desc = &img->funcs[func_index];
    SbcConstKind kind;
    const void *ptr;
    uint32_t len;
    if (sbc_const_at(img, desc->name_idx, &kind, &ptr, &len) && kind == SBC_CONST_STR) {
        return (const char*)ptr;
    }
    return NULL;
}

static const char *trap_code_name(uint16_t code) {
    switch ((SurgeTrapCode)code) {
        case SURGE_TRAP_UNREACHABLE: return "UNREACHABLE";
        case SURGE_TRAP_DIV_BY_ZERO: return "DIV_BY_ZERO";
        case SURGE_TRAP_OUT_OF_BOUNDS: return "OUT_OF_BOUNDS";
        case SURGE_TRAP_BAD_CALL: return "BAD_CALL";
        case SURGE_TRAP_TYPE_ERROR: return "TYPE_ERROR";
        default: return NULL;
    }
}

static size_t disasm_instruction(const SbcImage *img,
                                 const uint8_t *code,
                                 uint32_t offset,
                                 const uint8_t *end,
                                 FILE *out) {
    if (code >= end) {
        return 0;
    }

    uint8_t opcode_byte = *code;
    SurgeOpcode opcode = (SurgeOpcode)opcode_byte;
    const SurgeOpcodeInfo *info = surge_opcode_info(opcode);

    fprintf(out, "    %04u  ", offset);

    if (!info) {
        fprintf(out, "<invalid:%u>\n", opcode_byte);
        return 1;
    }

    fprintf(out, "%-12s", info->mnemonic);

    const uint8_t *cursor = code + 1;
    for (uint8_t i = 0; i < info->operand_count; ++i) {
        SurgeOperandKind kind = info->operands[i];
        size_t size = surge_operand_kind_size(kind);
        if (cursor + size > end) {
            fprintf(out, " <truncated>\n");
            return (size_t)(end - code);
        }
        if (i > 0) {
            fputs(", ", out);
        }
        switch (kind) {
            case SURGE_OPERAND_BOOL: {
                uint8_t v = cursor[0];
                fputs(v ? "true" : "false", out);
                break;
            }
            case SURGE_OPERAND_ARG_COUNT: {
                fprintf(out, "%u", (unsigned)cursor[0]);
                break;
            }
            case SURGE_OPERAND_LOCAL_SLOT:
            case SURGE_OPERAND_GLOBAL_SLOT: {
                uint16_t v = read_u16_le(cursor);
                fprintf(out, "%u", (unsigned)v);
                break;
            }
            case SURGE_OPERAND_FUNC_INDEX: {
                uint16_t v = read_u16_le(cursor);
                fprintf(out, "%u", (unsigned)v);
                const char *fn_name = resolve_func_name(img, v);
                if (fn_name) {
                    fprintf(out, " <%s>", fn_name);
                }
                break;
            }
            case SURGE_OPERAND_CONST_IDX: {
                uint32_t idx = read_u32_le(cursor);
                print_const_ref(img, idx, out);
                break;
            }
            case SURGE_OPERAND_ARRAY_COUNT: {
                uint32_t cnt = read_u32_le(cursor);
                fprintf(out, "%u", cnt);
                break;
            }
            case SURGE_OPERAND_TRAP_CODE: {
                uint16_t code_val = read_u16_le(cursor);
                fprintf(out, "%u", (unsigned)code_val);
                const char *name = trap_code_name(code_val);
                if (name) {
                    fprintf(out, " <%s>", name);
                }
                break;
            }
            case SURGE_OPERAND_JUMP_OFFSET: {
                int32_t delta = read_i32_le(cursor);
                fprintf(out, "%+d", delta);
                size_t inst_size = (size_t)((cursor - code) + size);
                int64_t target = (int64_t)offset + (int64_t)inst_size + (int64_t)delta;
                if (target >= 0) {
                    fprintf(out, " -> %04" PRId64, target);
                } else {
                    fprintf(out, " -> %" PRId64, target);
                }
                break;
            }
            case SURGE_OPERAND_I64: {
                int64_t v = (int64_t)read_u64_le(cursor);
                fprintf(out, "%" PRId64, v);
                break;
            }
            case SURGE_OPERAND_F64: {
                double d = read_f64_le(cursor);
                fprintf(out, "%g", d);
                break;
            }
            case SURGE_OPERAND_NONE:
                break;
            default:
                fputc('?', out);
                break;
        }
        cursor += size;
    }

    fputc('\n', out);
    return (size_t)(cursor - code);
}

static void dump_constants(const SbcImage *img, FILE *out) {
    fprintf(out, "Constants (count=%u):\n", img->const_count);
    for (uint32_t i = 0; i < img->const_count; ++i) {
        SbcConstKind kind;
        const void *payload;
        uint32_t len;
        if (!sbc_const_at(img, i, &kind, &payload, &len)) {
            fprintf(out, "  [%u] <invalid>\n", i);
            continue;
        }
        const uint8_t *bytes = (const uint8_t *)payload;
        switch (kind) {
            case SBC_CONST_I64: {
                int64_t v = (int64_t)read_u64_le(bytes);
                fprintf(out, "  [%u] i64 %" PRId64 "\n", i, v);
                break;
            }
            case SBC_CONST_F64: {
                double d = read_f64_le(bytes);
                fprintf(out, "  [%u] f64 %g\n", i, d);
                break;
            }
            case SBC_CONST_STR:
                fprintf(out, "  [%u] str ", i);
                print_string_escaped(out, (const char*)payload, len);
                fputc('\n', out);
                break;
            default:
                fprintf(out, "  [%u] kind=%u\n", i, (unsigned)kind);
                break;
        }
    }
}

static void dump_functions(const SbcImage *img, FILE *out) {
    fprintf(out, "Functions (count=%u):\n", img->func_count);
    for (uint32_t i = 0; i < img->func_count; ++i) {
        const SbcFuncDesc *fn = &img->funcs[i];
        const char *name = resolve_func_name(img, i);
        if (!name) {
            fprintf(out, "Function[%u] <const %u> : arity=%u locals=%u flags=0x%08X code=[%u..%u)\n",
                    i, fn->name_idx, fn->arity, fn->nlocals, fn->flags,
                    fn->code_off, fn->code_off + fn->code_len);
        } else {
            fprintf(out, "Function[%u] %s : arity=%u locals=%u flags=0x%08X code=[%u..%u)\n",
                    i, name, fn->arity, fn->nlocals, fn->flags,
                    fn->code_off, fn->code_off + fn->code_len);
        }

        if ((uint64_t)fn->code_off + fn->code_len > img->hdr.sz_code) {
            fprintf(out, "    <code out of range>\n");
            continue;
        }
        const uint8_t *code = img->code_sec + fn->code_off;
        const uint8_t *end = code + fn->code_len;
        uint32_t rel = 0;
        while (code < end) {
            size_t used = disasm_instruction(img, code, rel, end, out);
            if (used == 0) {
                fprintf(out, "    %04u  <incomplete>\n", rel);
                break;
            }
            code += used;
            rel += (uint32_t)used;
        }
    }
}

int surge_disasm_file(const char *path, FILE *out) {
    if (!path) {
        return 1;
    }
    if (!out) {
        out = stdout;
    }

    SbcImage img;
    if (!sbc_load_from_file(path, &img)) {
        fprintf(stderr, "surge: failed to load %s\n", path);
        return 1;
    }

    const SbcHeader *hdr = &img.hdr;
    fprintf(out, "SBC file: %s\n", path);
    fprintf(out, "  version : %u.%u\n", hdr->ver_major, hdr->ver_minor);
    fprintf(out, "  consts  : off=%u size=%u\n", hdr->off_const, hdr->sz_const);
    fprintf(out, "  funcs   : off=%u size=%u\n", hdr->off_funcs, hdr->sz_funcs);
    fprintf(out, "  code    : off=%u size=%u\n", hdr->off_code, hdr->sz_code);
    fprintf(out, "  globals : %u\n", img.global_count);
    fputc('\n', out);

    dump_constants(&img, out);
    fputc('\n', out);
    dump_functions(&img, out);

    sbc_unload(&img);
    return 0;
}
