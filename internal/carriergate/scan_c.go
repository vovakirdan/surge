package carriergate

import "regexp"

var (
	nativePayloadBitsRE = regexp.MustCompile(`\b(result_bits|resume_bits|value_bits|send_bits)\b`)
	nativeWordPointerRE = regexp.MustCompile(`\b(?:const\s+)?uint64_t\s*\*\s*(buf|out_bits|values)\b`)
	nativeMapKeyBitsRE  = regexp.MustCompile(`\buint64_t\s+(key_bits)\b`)
	nativeMapEntryRE    = regexp.MustCompile(`\buint64_t\s+(key|value)\s*;`)
	nativeDropRE        = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*_drop_fn_id|send_drop_fn_ids|__surge_drop_call|__surge_drop_result_call|__surge_drop_abandoned_state_call)\b`)
)

func scanCFile(filePath string, data []byte) []rawFinding {
	clean := sanitizeC(data)
	findings := make([]rawFinding, 0, 16)
	findings = appendCMatches(findings, categoryNativePayloadBits, filePath, data, clean, nativePayloadBitsRE)
	findings = appendCMatches(findings, categoryNativeWord, filePath, data, clean, nativeWordPointerRE)
	findings = appendCMatches(findings, categoryNativeWord, filePath, data, clean, nativeMapKeyBitsRE)
	findings = appendCMatches(findings, categoryNativeWord, filePath, data, clean, nativeMapEntryRE)
	findings = appendNativeWordContextMatches(findings, filePath, data, clean)
	findings = appendCMatches(findings, categoryNumericDrop, filePath, data, clean, nativeDropRE)
	return findings
}

func appendCMatches(out []rawFinding, category, filePath string, original, clean []byte, pattern *regexp.Regexp) []rawFinding {
	for _, match := range pattern.FindAllSubmatchIndex(clean, -1) {
		start, end := match[0], match[1]
		if len(match) >= 4 && match[2] >= 0 {
			start, end = match[2], match[3]
		}
		out = append(out, makeRawFinding(category, filePath, string(clean[start:end]), original, start))
	}
	return out
}

func sanitizeC(data []byte) []byte {
	clean := append([]byte(nil), data...)
	const (
		code = iota
		lineComment
		blockComment
		stringLiteral
		charLiteral
	)
	state := code
	for index := 0; index < len(data); index++ {
		current := data[index]
		next := byte(0)
		if index+1 < len(data) {
			next = data[index+1]
		}
		switch state {
		case code:
			switch {
			case current == '/' && next == '/':
				clean[index], clean[index+1] = ' ', ' '
				index++
				state = lineComment
			case current == '/' && next == '*':
				clean[index], clean[index+1] = ' ', ' '
				index++
				state = blockComment
			case current == '"':
				clean[index] = ' '
				state = stringLiteral
			case current == '\'':
				clean[index] = ' '
				state = charLiteral
			}
		case lineComment:
			switch {
			case current == '\\' && next == '\n':
				clean[index] = ' '
				index++
			case current == '\\' && next == '\r' && index+2 < len(data) && data[index+2] == '\n':
				clean[index] = ' '
				index += 2
			case current == '\n':
				state = code
			default:
				clean[index] = ' '
			}
		case blockComment:
			if current == '*' && next == '/' {
				clean[index], clean[index+1] = ' ', ' '
				index++
				state = code
			} else if current != '\n' {
				clean[index] = ' '
			}
		case stringLiteral, charLiteral:
			quote := byte('"')
			if state == charLiteral {
				quote = '\''
			}
			switch {
			case current == '\\' && next != 0:
				clean[index] = ' '
				if next != '\n' {
					clean[index+1] = ' '
				}
				index++
			case current == quote:
				clean[index] = ' '
				state = code
			case current != '\n':
				clean[index] = ' '
			}
		}
	}
	return clean
}
