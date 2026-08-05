# Count effective source lines, or emit comment-stripped lines with mode=lines.
# Quote and block-comment state intentionally match check_file_sizes.sh's
# original counter so extracting this helper does not change the old gate.
BEGIN {
    if (mode == "") {
        mode = "count"
    }
}

function strip_comments(line,    i, ch, next_ch, out) {
    out = ""
    for (i = 1; i <= length(line); i++) {
        ch = substr(line, i, 1)
        next_ch = substr(line, i + 1, 1)

        if (in_block_comment) {
            if (ch == "*" && next_ch == "/") {
                in_block_comment = 0
                i++
            }
            continue
        }

        if (quote != "") {
            out = out ch
            if (quote == "`") {
                if (ch == "`") {
                    quote = ""
                }
            } else if (escaped) {
                escaped = 0
            } else if (ch == "\\") {
                escaped = 1
            } else if (ch == quote) {
                quote = ""
            }
            continue
        }

        if (ch == "\"" || ch == "\047" || ch == "`") {
            out = out ch
            quote = ch
            escaped = 0
            continue
        }
        if (ch == "/" && next_ch == "/") {
            break
        }
        if (ch == "/" && next_ch == "*") {
            in_block_comment = 1
            i++
            continue
        }
        out = out ch
    }

    if (quote != "`") {
        quote = ""
        escaped = 0
    }
    sub(/^[[:space:]]+/, "", out)
    sub(/[[:space:]]+$/, "", out)
    return out
}

{
    effective_line = strip_comments($0)
    if (effective_line ~ /[^[:space:]]/) {
        if (mode == "lines") {
            print effective_line
        } else {
            count++
        }
    }
}

END {
    if (mode != "lines") {
        print count + 0
    }
}
