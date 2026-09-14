package symbols

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/types"
)

// FunctionTypeKey preserves physical formal slots and their declared relation.
// The metadata spelling is an internal key, not additional source syntax.
func FunctionTypeKey(params []TypeKey, result TypeKey, sources types.ReturnSources) TypeKey {
	if result == "" {
		return ""
	}
	parts := make([]string, len(params))
	for i, param := range params {
		if param == "" {
			return ""
		}
		parts[i] = string(param)
	}
	for _, slot := range sources.Slots() {
		if uint64(slot) >= uint64(len(params)) {
			panic(fmt.Errorf("function type key: return source %d outside arity %d", slot, len(params)))
		}
	}
	return TypeKey("fn" + sources.CanonicalKey() + "(" + strings.Join(parts, ",") + ")->" + string(result))
}

// IsFunctionTypeKey includes malformed metadata prefixes so callers refuse
// them rather than trying to recover a different type or an AllInputs promise.
func IsFunctionTypeKey(key TypeKey) bool {
	s := strings.TrimSpace(string(key))
	return strings.HasPrefix(s, "fn(") || strings.HasPrefix(s, "fn@return_source")
}

// ParseFunctionTypeKey finds the outer parameter list before its result arrow.
// Parameter keys remain opaque to this layer; the type resolver handles them.
func ParseFunctionTypeKey(key TypeKey) (params []TypeKey, result TypeKey, sources types.ReturnSources, ok bool) {
	s := strings.TrimSpace(string(key))
	if !strings.HasPrefix(s, "fn") {
		return nil, "", sources, false
	}
	s = strings.TrimPrefix(s, "fn")
	if strings.HasPrefix(s, "@") {
		var parsed bool
		sources, s, parsed = parseReturnSourceKey(s)
		if !parsed {
			return nil, "", types.ReturnSources{}, false
		}
	}
	if len(s) == 0 || s[0] != '(' {
		return nil, "", sources, false
	}
	stack := []byte{')'}
	start := 1
	for i := 1; i < len(s); i++ {
		if s[i] == ',' && len(stack) == 1 {
			part := strings.TrimSpace(s[start:i])
			if part == "" {
				return nil, "", sources, false
			}
			params = append(params, TypeKey(part))
			start = i + 1
			continue
		}
		if !keyDelimiter(s, i, &stack) {
			return nil, "", sources, false
		}
		if len(stack) != 0 {
			continue
		}
		part := strings.TrimSpace(s[start:i])
		if part != "" {
			params = append(params, TypeKey(part))
		} else if len(params) != 0 {
			return nil, "", sources, false
		}
		tail := strings.TrimSpace(s[i+1:])
		if !strings.HasPrefix(tail, "->") {
			return nil, "", sources, false
		}
		result = TypeKey(strings.TrimSpace(tail[2:]))
		if result == "" || !balancedTypeKey(string(result)) {
			return nil, "", sources, false
		}
		for _, slot := range sources.Slots() {
			if uint64(slot) >= uint64(len(params)) {
				return nil, "", sources, false
			}
		}
		return params, result, sources, true
	}
	return nil, "", sources, false
}

func parseReturnSourceKey(s string) (types.ReturnSources, string, bool) {
	const prefix = "@return_source{"
	if !strings.HasPrefix(s, prefix) {
		return types.ReturnSources{}, "", false
	}
	end := strings.IndexByte(s, '}')
	if end < len(prefix) {
		return types.ReturnSources{}, "", false
	}
	var slots []uint32
	if text := s[len(prefix):end]; text != "" {
		for _, part := range strings.Split(text, ",") {
			slot, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return types.ReturnSources{}, "", false
			}
			slots = append(slots, uint32(slot))
		}
	}
	return types.ExplicitReturnSources(slots...), s[end+1:], true
}

func keyDelimiter(s string, i int, stack *[]byte) bool {
	var close byte
	switch s[i] {
	case '(':
		close = ')'
	case '[':
		close = ']'
	case '<':
		close = '>'
	case '{':
		close = '}'
	case ')', ']', '>', '}':
		if s[i] == '>' && i > 0 && s[i-1] == '-' {
			return true
		}
		if len(*stack) == 0 || (*stack)[len(*stack)-1] != s[i] {
			return false
		}
		*stack = (*stack)[:len(*stack)-1]
	}
	if close != 0 {
		*stack = append(*stack, close)
	}
	return true
}

func balancedTypeKey(s string) bool {
	var stack []byte
	for i := range len(s) {
		if !keyDelimiter(s, i, &stack) {
			return false
		}
	}
	return len(stack) == 0
}

// functionShapeKey omits only declared return-source metadata. Overload
// declaration equality stays a comparison of ordinary parameter/result shapes.
func functionShapeKey(key TypeKey) TypeKey {
	s := string(key)
	const prefix = "fn@return_source"
	var out strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			out.WriteString(s)
			return TypeKey(out.String())
		}
		_, tail, ok := parseReturnSourceKey(s[i+2:])
		if !ok || !strings.HasPrefix(tail, "(") {
			return key
		}
		out.WriteString(s[:i+2])
		s = tail
	}
}
