package carriergate

import (
	"bytes"
	"regexp"
)

var (
	nativeDirectWordAliasRE = regexp.MustCompile(
		`\buint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*[^\n;]*\b(?:result_bits|resume_bits|value_bits|send_bits)\b`,
	)
	nativeBlockingWordAliasRE = regexp.MustCompile(
		`\buint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*__surge_blocking_call\s*\(`,
	)
	nativeReturnWordParamRE = regexp.MustCompile(
		`\brt_async_return\s*\(\s*[^,;{}]+,\s*uint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*\)`,
	)
	nativeMapEntryWordParamRE = regexp.MustCompile(
		`\bmap_key_eq\s*\(\s*[^,;{}]+,\s*uint64_t\s+[A-Za-z_][A-Za-z0-9_]*\s*,\s*uint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*\)`,
	)
	nativeMapPreviousWordParamRE = regexp.MustCompile(
		`\brt_map_(?:insert|remove)\s*\([^;{}]*,\s*uint64_t\s*\*\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`,
	)
	nativeSelectReturnWordParamRE = regexp.MustCompile(
		`\b(?:select_return_arms|select_finish_retry)\s*\(\s*[^,;{}]+,\s*uint64_t\s*\*\s*([A-Za-z_][A-Za-z0-9_]*)\b`,
	)
	nativeZeroWordDeclRE = regexp.MustCompile(
		`\buint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*0\s*;`,
	)
	nativeWordArrayDeclRE = regexp.MustCompile(
		`\buint64_t\s+([A-Za-z_][A-Za-z0-9_]*)\s*\[[^\]\n]+\]\s*;`,
	)
	nativeBufferWordSinkRE = regexp.MustCompile(
		`\bbuf_pop\s*\([^,;{}]+,\s*&\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`,
	)
	nativeAssignedWordSinkRE = regexp.MustCompile(
		`\b([A-Za-z_][A-Za-z0-9_]*)\s*=\s*[^;\n]*\b(?:result_bits|resume_bits|value_bits|send_bits)\b`,
	)
	nativeArrayWordSinkRE = regexp.MustCompile(
		`\b([A-Za-z_][A-Za-z0-9_]*)\s*\[[^\]\n]+\]\s*=\s*[^;\n]*\b(?:result_bits|resume_bits|value_bits|send_bits)\b`,
	)
)

var nativeWordContextPatterns = []*regexp.Regexp{
	nativeDirectWordAliasRE,
	nativeBlockingWordAliasRE,
	nativeReturnWordParamRE,
	nativeMapEntryWordParamRE,
	nativeMapPreviousWordParamRE,
	nativeSelectReturnWordParamRE,
}

func appendNativeWordContextMatches(out []rawFinding, filePath string, original, clean []byte) []rawFinding {
	for _, pattern := range nativeWordContextPatterns {
		out = appendCWordContextMatches(out, filePath, original, clean, pattern)
	}
	out = appendCFlowWordMatches(
		out, filePath, original, clean, nativeZeroWordDeclRE,
		[]*regexp.Regexp{nativeBufferWordSinkRE, nativeAssignedWordSinkRE},
	)
	return appendCFlowWordMatches(
		out, filePath, original, clean, nativeWordArrayDeclRE,
		[]*regexp.Regexp{nativeArrayWordSinkRE},
	)
}

func appendCWordContextMatches(
	out []rawFinding,
	filePath string,
	original, clean []byte,
	pattern *regexp.Regexp,
) []rawFinding {
	for _, match := range pattern.FindAllSubmatchIndex(clean, -1) {
		nameStart, nameEnd := match[2], match[3]
		name := clean[nameStart:nameEnd]
		if nativeWordNameAlreadyTracked(name) {
			continue
		}
		out = append(out, makeRawFinding(
			categoryNativeWord, filePath, string(name), original, nameStart,
		))
	}
	return out
}

func appendCFlowWordMatches(
	out []rawFinding,
	filePath string,
	original, clean []byte,
	declaration *regexp.Regexp,
	sinks []*regexp.Regexp,
) []rawFinding {
	for _, match := range declaration.FindAllSubmatchIndex(clean, -1) {
		nameStart, nameEnd := match[2], match[3]
		name := clean[nameStart:nameEnd]
		if nativeWordNameAlreadyTracked(name) {
			continue
		}
		blockEnd := enclosingCBlockEnd(clean, match[1])
		for _, sink := range sinks {
			if cFlowSinkMatchesName(clean[match[1]:blockEnd], sink, name) {
				out = append(out, makeRawFinding(
					categoryNativeWord, filePath, string(name), original, nameStart,
				))
				break
			}
		}
	}
	return out
}

func nativeWordNameAlreadyTracked(name []byte) bool {
	return bytes.Equal(name, []byte("key_bits")) || nativePayloadBitsRE.Match(name)
}

func cFlowSinkMatchesName(scope []byte, sink *regexp.Regexp, name []byte) bool {
	for _, match := range sink.FindAllSubmatchIndex(scope, -1) {
		if bytes.Equal(scope[match[2]:match[3]], name) {
			return true
		}
	}
	return false
}

func enclosingCBlockEnd(clean []byte, offset int) int {
	depth := 0
	for index := range offset {
		switch clean[index] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth == 0 {
		return offset
	}
	for relative := range len(clean) - offset {
		index := offset + relative
		switch clean[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return len(clean)
}
